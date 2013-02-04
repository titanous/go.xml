// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xml

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"
	"time"
)

const (
	// A generic XML header suitable for use with the output of Marshal.
	// This is not automatically added to any output of this package,
	// it is provided as a convenience.
	Header = `<?xml version="1.0" encoding="UTF-8"?>` + "\n"
)

// Marshal returns the XML encoding of v.
//
// Marshal handles an array or slice by marshalling each of the elements.
// Marshal handles a pointer by marshalling the value it points at or, if the
// pointer is nil, by writing nothing.  Marshal handles an interface value by
// marshalling the value it contains or, if the interface value is nil, by
// writing nothing.  Marshal handles all other data by writing one or more XML
// elements containing the data.
//
// The name for the XML elements is taken from, in order of preference:
//     - the tag on the XMLName field, if the data is a struct
//     - the value of the XMLName field of type xml.Name
//     - the tag of the struct field used to obtain the data
//     - the name of the struct field used to obtain the data
//     - the name of the marshalled type
//
// The XML element for a struct contains marshalled elements for each of the
// exported fields of the struct, with these exceptions:
//     - the XMLName field, described above, is omitted.
//     - a field with tag "-" is omitted.
//     - a field with tag "name,attr" becomes an attribute with
//       the given name in the XML element.
//     - a field with tag ",attr" becomes an attribute with the
//       field name in the XML element.
//     - a field with tag ",chardata" is written as character data,
//       not as an XML element.
//     - a field with tag ",innerxml" is written verbatim, not subject
//       to the usual marshalling procedure.
//     - a field with tag ",comment" is written as an XML comment, not
//       subject to the usual marshalling procedure. It must not contain
//       the "--" string within it.
//     - a field with a tag including the "omitempty" option is omitted
//       if the field value is empty. The empty values are false, 0, any
//       nil pointer or interface value, and any array, slice, map, or
//       string of length zero.
//     - an anonymous struct field is handled as if the fields of its
//       value were part of the outer struct.
//
// If a field uses a tag "a>b>c", then the element c will be nested inside
// parent elements a and b.  Fields that appear next to each other that name
// the same parent will be enclosed in one XML element.
//
// See MarshalIndent for an example.
//
// Marshal will return an error if asked to marshal a channel, function, or map.
func Marshal(v interface{}) ([]byte, error) {
	var b bytes.Buffer
	if err := NewEncoder(&b).Encode(v); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// MarshalIndent works like Marshal, but each XML element begins on a new
// indented line that starts with prefix and is followed by one or more
// copies of indent according to the nesting depth.
func MarshalIndent(v interface{}, prefix, indent string) ([]byte, error) {
	var b bytes.Buffer
	enc := NewEncoder(&b)
	enc.Indent(prefix, indent)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// An Encoder writes XML data to an output stream.
type Encoder struct {
	context *context
	printer
}

// NewEncoder returns a new encoder that writes to w.
func NewEncoder(w io.Writer) *Encoder {
	return &Encoder{rootNs2Pfx(), printer{Writer: bufio.NewWriter(w)}}
}

// Indent sets the encoder to generate XML in which each element
// begins on a new indented line that starts with prefix and is followed by
// one or more copies of indent according to the nesting depth.
func (enc *Encoder) Indent(prefix, indent string) {
	enc.prefix = prefix
	enc.indent = indent
}

// Encode writes the XML encoding of v to the stream.
//
// See the documentation for Marshal for details about the conversion
// of Go values to XML.
func (enc *Encoder) Encode(v interface{}) error {
	err := enc.marshalValue(reflect.ValueOf(v), nil, enc.context.child())
	if err != nil {
		return err
	}
	return enc.Flush()
}

type printer struct {
	*bufio.Writer
	indent     string
	prefix     string
	depth      int
	indentedIn bool
	putNewline bool
}

// marshalValue writes one or more XML elements representing val.
// If val was obtained from a struct field, finfo must have its details.
// nsctx is a namespace-to-prefix mapping to use.
func (p *printer) marshalValue(val reflect.Value, finfo *fieldInfo, nsctx *context) error {
	if !val.IsValid() {
		return nil
	}
	if finfo != nil && finfo.flags&fOmitEmpty != 0 && isEmptyValue(val) {
		return nil
	}

	kind := val.Kind()
	typ := val.Type()

	// Drill into pointers/interfaces
	if kind == reflect.Ptr || kind == reflect.Interface {
		if val.IsNil() {
			return nil
		}
		return p.marshalValue(val.Elem(), finfo, nsctx)
	}

	// Slices and arrays iterate over the elements. They do not have an enclosing tag.
	if (kind == reflect.Slice || kind == reflect.Array) && typ.Elem().Kind() != reflect.Uint8 {
		for i, n := 0, val.Len(); i < n; i++ {
			if err := p.marshalValue(val.Index(i), finfo, nsctx.child()); err != nil {
				return err
			}
		}
		return nil
	}

	if finfo != nil {
		nsctx.xmlns = finfo.xmlns
	}

	tinfo, err := getTypeInfo(typ, nsctx.xmlns)
	if err != nil {
		return err
	}

	// Precedence for the XML element name is:
	// 1. XMLName field in underlying struct;
	// 2. field name/tag in the struct field; and
	// 3. type name
	var pfx, name string
	if tinfo.xmlname != nil {
		// BUG(cjyar): It's not possible to specify a
		// prefix-to-namespace mapping at runtime, via the
		// xml.Name structure.
		xmlname := tinfo.xmlname
		if xmlname.name != "" {
			nsctx.xmlns, name = xmlname.xmlns, xmlname.name
		} else if v, ok := xmlname.value(val).Interface().(Name); ok && v.Local != "" {
			nsctx.xmlns, name = v.Space, v.Local
		}
	}
	if name == "" && finfo != nil {
		pfx, name = finfo.prefix, finfo.name
	}
	if name == "" {
		name = typ.Name()
		if name == "" {
			return &UnsupportedTypeError{typ}
		}
	}

	// Write the name. Cases to handle:
	// - Simple name, no namespace or prefix.
	// - Name with namespace, no prefix, no mapping for namespace.
	// - Name with namespace, no prefix, but we have a ns->pfx mapping.
	// - Name with namespace and prefix.

	mapPfx, isMapped := nsctx.Get(nsctx.xmlns)
	p.writeIndent(1)
	p.WriteByte('<')
	if pfx != "" {
		p.WriteString(pfx)
		p.WriteByte(':')
	} else if mapPfx != "" {
		p.WriteString(mapPfx)
		p.WriteByte(':')
	}
	p.WriteString(name)

	if nsctx.xmlns != "" && !isMapped {
		nsctx.pfxmap[nsctx.xmlns] = pfx
		p.WriteString(" xmlns")
		if pfx != "" {
			p.WriteByte(':')
			p.WriteString(pfx)
		}
		p.WriteString(`="`)
		// TODO: EscapeString, to avoid the allocation.
		Escape(p, []byte(nsctx.xmlns))
		p.WriteByte('"')
	}

	// Scan attributes for new namespaces before outputting the attributes.
	for _, attrInfo := range tinfo.fields {
		if attrInfo.flags&fAttr == 0 {
			continue
		}
		fv := attrInfo.value(val)
		if attrInfo.flags&fOmitEmpty != 0 && isEmptyValue(fv) {
			continue
		}
		_, attrIsMapped := nsctx.Get(attrInfo.xmlns)
		if attrInfo.xmlns != "" && !attrIsMapped {
			if attrInfo.prefix == "" {
				return fmt.Errorf("Attribute %s of %s needs a prefix", attrInfo.name, name)
			}
			nsctx.pfxmap[attrInfo.xmlns] = attrInfo.prefix
			p.WriteString(" xmlns:")
			p.WriteString(attrInfo.prefix)
			p.WriteString(`="`)
			Escape(p, []byte(attrInfo.xmlns))
			p.WriteByte('"')
		}
	}

	// Attributes
	for _, attrInfo := range tinfo.fields {
		if attrInfo.flags&fAttr == 0 {
			continue
		}
		fv := attrInfo.value(val)
		if attrInfo.flags&fOmitEmpty != 0 && isEmptyValue(fv) {
			continue
		}
		p.WriteByte(' ')
		prefix, _ := nsctx.Get(attrInfo.xmlns)
		if prefix != "" {
			p.WriteString(prefix)
			p.WriteByte(':')
		}
		p.WriteString(attrInfo.name)
		p.WriteString(`="`)
		if err := p.marshalSimple(fv.Type(), fv); err != nil {
			return err
		}
		p.WriteByte('"')
	}
	p.WriteByte('>')

	if val.Kind() == reflect.Struct {
		err = p.marshalStruct(tinfo, val, nsctx)
	} else {
		err = p.marshalSimple(typ, val)
	}
	if err != nil {
		return err
	}

	p.writeIndent(-1)
	p.WriteByte('<')
	p.WriteByte('/')
	if pfx != "" {
		p.WriteString(pfx)
		p.WriteByte(':')
	} else if mapPfx != "" {
		p.WriteString(mapPfx)
		p.WriteByte(':')
	}
	p.WriteString(name)
	p.WriteByte('>')

	return p.cachedWriteError()
}

var timeType = reflect.TypeOf(time.Time{})

func (p *printer) marshalSimple(typ reflect.Type, val reflect.Value) error {
	// Normally we don't see structs, but this can happen for an attribute.
	if val.Type() == timeType {
		p.WriteString(val.Interface().(time.Time).Format(time.RFC3339Nano))
		return nil
	}
	switch val.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		p.WriteString(strconv.FormatInt(val.Int(), 10))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		p.WriteString(strconv.FormatUint(val.Uint(), 10))
	case reflect.Float32, reflect.Float64:
		p.WriteString(strconv.FormatFloat(val.Float(), 'g', -1, val.Type().Bits()))
	case reflect.String:
		// TODO: Add EscapeString.
		Escape(p, []byte(val.String()))
	case reflect.Bool:
		p.WriteString(strconv.FormatBool(val.Bool()))
	case reflect.Array:
		// will be [...]byte
		bytes := make([]byte, val.Len())
		for i := range bytes {
			bytes[i] = val.Index(i).Interface().(byte)
		}
		Escape(p, bytes)
	case reflect.Slice:
		// will be []byte
		Escape(p, val.Bytes())
	default:
		return &UnsupportedTypeError{typ}
	}
	return p.cachedWriteError()
}

var ddBytes = []byte("--")

func (p *printer) marshalStruct(tinfo *typeInfo, val reflect.Value, nsctx *context) error {
	if val.Type() == timeType {
		_, err := p.WriteString(val.Interface().(time.Time).Format(time.RFC3339Nano))
		return err
	}
	s := parentStack{printer: p}
	for i := range tinfo.fields {
		finfo := &tinfo.fields[i]
		if finfo.flags&(fAttr) != 0 {
			continue
		}
		vf := finfo.value(val)
		switch finfo.flags & fMode {
		case fCharData:
			var scratch [64]byte
			switch vf.Kind() {
			case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
				Escape(p, strconv.AppendInt(scratch[:0], vf.Int(), 10))
			case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
				Escape(p, strconv.AppendUint(scratch[:0], vf.Uint(), 10))
			case reflect.Float32, reflect.Float64:
				Escape(p, strconv.AppendFloat(scratch[:0], vf.Float(), 'g', -1, vf.Type().Bits()))
			case reflect.Bool:
				Escape(p, strconv.AppendBool(scratch[:0], vf.Bool()))
			case reflect.String:
				Escape(p, []byte(vf.String()))
			case reflect.Slice:
				if elem, ok := vf.Interface().([]byte); ok {
					Escape(p, elem)
				}
			case reflect.Struct:
				if vf.Type() == timeType {
					Escape(p, []byte(vf.Interface().(time.Time).Format(time.RFC3339Nano)))
				}
			}
			continue

		case fComment:
			k := vf.Kind()
			if !(k == reflect.String || k == reflect.Slice && vf.Type().Elem().Kind() == reflect.Uint8) {
				return fmt.Errorf("xml: bad type for comment field of %s", val.Type())
			}
			if vf.Len() == 0 {
				continue
			}
			p.writeIndent(0)
			p.WriteString("<!--")
			dashDash := false
			dashLast := false
			switch k {
			case reflect.String:
				s := vf.String()
				dashDash = strings.Index(s, "--") >= 0
				dashLast = s[len(s)-1] == '-'
				if !dashDash {
					p.WriteString(s)
				}
			case reflect.Slice:
				b := vf.Bytes()
				dashDash = bytes.Index(b, ddBytes) >= 0
				dashLast = b[len(b)-1] == '-'
				if !dashDash {
					p.Write(b)
				}
			default:
				panic("can't happen")
			}
			if dashDash {
				return fmt.Errorf(`xml: comments must not contain "--"`)
			}
			if dashLast {
				// "--->" is invalid grammar. Make it "- -->"
				p.WriteByte(' ')
			}
			p.WriteString("-->")
			continue

		case fInnerXml:
			iface := vf.Interface()
			switch raw := iface.(type) {
			case []byte:
				p.Write(raw)
				continue
			case string:
				p.WriteString(raw)
				continue
			}

		case fElement, fElement | fAny:
			s.trim(finfo.parents)
			if len(finfo.parents) > len(s.stack) {
				if vf.Kind() != reflect.Ptr && vf.Kind() != reflect.Interface || !vf.IsNil() {
					s.push(finfo.parents[len(s.stack):])
				}
			}
		}
		if err := p.marshalValue(vf, finfo, nsctx.child()); err != nil {
			return err
		}
	}
	s.trim(nil)
	return p.cachedWriteError()
}

// return the bufio Writer's cached write error
func (p *printer) cachedWriteError() error {
	_, err := p.Write(nil)
	return err
}

func (p *printer) writeIndent(depthDelta int) {
	if len(p.prefix) == 0 && len(p.indent) == 0 {
		return
	}
	if depthDelta < 0 {
		p.depth--
		if p.indentedIn {
			p.indentedIn = false
			return
		}
		p.indentedIn = false
	}
	if p.putNewline {
		p.WriteByte('\n')
	} else {
		p.putNewline = true
	}
	if len(p.prefix) > 0 {
		p.WriteString(p.prefix)
	}
	if len(p.indent) > 0 {
		for i := 0; i < p.depth; i++ {
			p.WriteString(p.indent)
		}
	}
	if depthDelta > 0 {
		p.depth++
		p.indentedIn = true
	}
}

type parentStack struct {
	*printer
	stack []string
}

// trim updates the XML context to match the longest common prefix of the stack
// and the given parents.  A closing tag will be written for every parent
// popped.  Passing a zero slice or nil will close all the elements.
func (s *parentStack) trim(parents []string) {
	split := 0
	for ; split < len(parents) && split < len(s.stack); split++ {
		if parents[split] != s.stack[split] {
			break
		}
	}
	for i := len(s.stack) - 1; i >= split; i-- {
		s.writeIndent(-1)
		s.WriteString("</")
		s.WriteString(s.stack[i])
		s.WriteByte('>')
	}
	s.stack = parents[:split]
}

// push adds parent elements to the stack and writes open tags.
func (s *parentStack) push(parents []string) {
	for i := 0; i < len(parents); i++ {
		s.writeIndent(1)
		s.WriteByte('<')
		s.WriteString(parents[i])
		s.WriteByte('>')
	}
	s.stack = append(s.stack, parents...)
}

// A MarshalXMLError is returned when Marshal encounters a type
// that cannot be converted into XML.
type UnsupportedTypeError struct {
	Type reflect.Type
}

func (e *UnsupportedTypeError) Error() string {
	return "xml: unsupported type: " + e.Type.String()
}

func isEmptyValue(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
		return v.Len() == 0
	case reflect.Bool:
		return !v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return v.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	case reflect.Interface, reflect.Ptr:
		return v.IsNil()
	}
	return false
}

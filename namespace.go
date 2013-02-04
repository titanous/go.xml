// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xml

const (
	XML_NS   = "http://www.w3.org/XML/1998/namespace"
	XMLNS_NS = "http://www.w3.org/2000/xmlns/"
)

// Track namespace context. This includes the namespace of the current
// element, and also the mapping between namespace prefixes and
// namespace URIs. The marshaler needs ns -> pfx, and the unmarshaler
// needs the opposite. Each element's associated context inherits from
// its parent's context.
type context struct {
	// xmlns is the namespace (URI) of the current element.
	xmlns string
	// pfxmap is the mapping between prefix and namespace of the
	// current element. Ancestor elements have their own
	// associated maps whose entries are not visible here. If this
	// object is associated with an Encoder, its pfxmap is
	// namespace -> prefix; if a Decoder, then prefix ->
	// namespace. Use this object to add new mappings, and the Get
	// method to read the current mapping.
	pfxmap map[string]string
	parent *context
}

// Get reads the mapping for this element, including the mappings for
// all its parent elements. The second return value will be true if
// the mapping was found.
func (n *context) Get(k string) (string, bool) {
	if v, ok := n.pfxmap[k]; ok {
		return v, ok
	}

	if n.parent != nil && n.parent != n {
		return n.parent.Get(k)
	}

	return "", false
}

func (n *context) child() *context {
	child := &context{}
	child.pfxmap = make(map[string]string)
	child.parent = n
	return child
}

func rootNs2Pfx() *context {
	n := &context{}
	n.pfxmap = make(map[string]string)
	n.parent = n
	n.pfxmap[XML_NS] = "xml"
	n.pfxmap[XMLNS_NS] = "xmlns"
	return n
}

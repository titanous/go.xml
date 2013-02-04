// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xml

// Tests specific to the context object which is included in Encoder
// and Decoder.

import (
	"fmt"
	"io"
	"strings"
)

type XmppErr struct {
	XMLName Name       `xml:"xmpp1 error"`
	Text    XmppErrTxt `xml:",any"`
}

type XmppErrTxt struct {
	XMLName Name
}

var decodectxstr = `<stream:error><blah xmlns="xmpp2"></blah></stream:error>`

// Imagine we need to read from the middle of an XML document, after
// namespaces and prefixes have already been declared. It's possible
// to load that context into the Decoder.
func ExampleDecoder_context() {
	datardr := strings.NewReader(decodectxstr)
	// Make a dummy start-tag with the document's namespace
	// declarations.
	nsstr := `<a xmlns="xmpp0" xmlns:stream="xmpp1">`
	nsrdr := strings.NewReader(nsstr)
	r := io.MultiReader(nsrdr, datardr)
	dec := NewDecoder(r)
	// Read and discard the start-tag; this has the side effect of
	// loading namespace prefixes.
	dec.Token()
	val := &XmppErr{}
	err := dec.Decode(val)
	if err != nil {
		fmt.Printf("Decode: %s", err)
		return
	}
	fmt.Printf("%#v", val)
	// Output:
	// &xml.XmppErr{XMLName:xml.Name{Space:"xmpp1", Local:"error"}, Text:xml.XmppErrTxt{XMLName:xml.Name{Space:"xmpp2", Local:"blah"}}}
}

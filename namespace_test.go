// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xml

// Tests specific to the context object which is included in Encoder
// and Decoder.

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

func TestMarshalSeries(t *testing.T) {
	obj1 := XmppErr{}
	obj2 := XmppErr{}
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	err := enc.Encode(obj1)
	if err != nil {
		t.Fatalf("Encode %v: %s", obj1, err)
	}
	err = enc.Encode(obj2)
	if err != nil {
		t.Fatalf("Encode %v: %s", obj2, err)
	}
	want := `<error xmlns="xmpp1"><Text></Text></error>` +
		`<error xmlns="xmpp1"><Text></Text></error>`
	got := buf.String()
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestUnmarshalSeries(t *testing.T) {
	str := `<error xmlns="xmpp1"></error>` +
		`<error xmlns="xmpp1"></error>`
	r := strings.NewReader(str)
	dec := NewDecoder(r)
	obj1 := &XmppErr{}
	obj2 := &XmppErr{}
	err := dec.Decode(obj1)
	if err != nil {
		t.Fatalf("Decode %s: %s", str, err)
	}
	err = dec.Decode(obj2)
	if err != nil {
		t.Fatalf("Decode %s: %s", str, err)
	}
	want1 := &XmppErr{XMLName: Name{Space: "xmpp1", Local: "error"}}
	want2 := &XmppErr{XMLName: Name{Space: "xmpp1", Local: "error"}}
	if !reflect.DeepEqual(obj1, want1) {
		t.Errorf("#1 unmarshal(%q):\nhave %#v\nwant %#v", str, obj1,
			want1)
	}
	if !reflect.DeepEqual(obj2, want2) {
		t.Errorf("#1 unmarshal(%q):\nhave %#v\nwant %#v", str, obj2,
			want2)
	}
}

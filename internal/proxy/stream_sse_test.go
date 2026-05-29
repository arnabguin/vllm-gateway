package proxy

import "testing"

func TestSSEParserSingleEvent(t *testing.T) {
	p := NewSSEParser()
	got := p.Push([]byte("data: hello\n\n"))
	if len(got) != 1 || got[0] != "hello" {
		t.Fatalf("unexpected payloads: %#v", got)
	}
}

func TestSSEParserSplitAcrossPushes(t *testing.T) {
	p := NewSSEParser()
	got := p.Push([]byte("data: hel"))
	if len(got) != 0 {
		t.Fatalf("expected no complete event, got %#v", got)
	}
	got = p.Push([]byte("lo\n\n"))
	if len(got) != 1 || got[0] != "hello" {
		t.Fatalf("unexpected payloads: %#v", got)
	}
}

func TestSSEParserMultipleEventsInOneChunk(t *testing.T) {
	p := NewSSEParser()
	got := p.Push([]byte("data: one\n\ndata: two\n\n"))
	if len(got) != 2 || got[0] != "one" || got[1] != "two" {
		t.Fatalf("unexpected payloads: %#v", got)
	}
}

func TestSSEParserMultiLineData(t *testing.T) {
	p := NewSSEParser()
	got := p.Push([]byte("data: line1\ndata: line2\n\n"))
	if len(got) != 1 || got[0] != "line1\nline2" {
		t.Fatalf("unexpected payloads: %#v", got)
	}
}

func TestSSEParserIgnoresCommentsAndFields(t *testing.T) {
	p := NewSSEParser()
	got := p.Push([]byte(":keepalive\nevent: message\nid: 1\ndata: payload\n\n"))
	if len(got) != 1 || got[0] != "payload" {
		t.Fatalf("unexpected payloads: %#v", got)
	}
}

func TestSSEParserCRLF(t *testing.T) {
	p := NewSSEParser()
	got := p.Push([]byte("data: hello\r\n\r\n"))
	if len(got) != 1 || got[0] != "hello" {
		t.Fatalf("unexpected payloads: %#v", got)
	}
}

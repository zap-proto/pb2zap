package pb2zap

import (
	"strings"
	"testing"
)

func TestRewrite_Construction(t *testing.T) {
	src := `package x

import (
	"context"

	mount_pb "example.com/pb/mount_pb"
)

func do(ctx context.Context) {
	a := &mount_pb.ConfigureRequest{CollectionCapacity: 5}
	b := mount_pb.ConfigureResponse{}
	_, _ = a, b
}
`
	rules := []Rule{{PB: "example.com/pb/mount_pb", Wire: "example.com/wire/mount", Name: "mountwire"}}
	out, changed, pbLeft, err := Rewrite("x.go", []byte(src), rules)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected changed")
	}
	s := string(out)
	for _, want := range []string{
		"mountwire.NewConfigureRequest(mountwire.ConfigureRequestInput{CollectionCapacity: 5})",
		"mountwire.NewConfigureResponse(mountwire.ConfigureResponseInput{})",
		`mountwire "example.com/wire/mount"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in:\n%s", want, s)
		}
	}
	if strings.Contains(s, "mount_pb") {
		t.Errorf("mount_pb import should be gone:\n%s", s)
	}
	if pbLeft != 0 {
		t.Errorf("pbLeft = %d, want 0", pbLeft)
	}
}

// TestRewrite_KeepsImportWhenStillUsed proves pb2zap rewrites construction but
// keeps the pb import (and reports pbLeft) when other pb uses remain — it never
// guesses field reads.
func TestRewrite_KeepsImportWhenStillUsed(t *testing.T) {
	src := `package x

import filer_pb "example.com/pb/filer_pb"

func do(e *filer_pb.Entry) []byte {
	r := &filer_pb.LookupRequest{Name: e.Name}
	return r
}
`
	rules := []Rule{{PB: "example.com/pb/filer_pb", Wire: "example.com/wire/filer", Name: "filerwire"}}
	out, changed, pbLeft, err := Rewrite("x.go", []byte(src), rules)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected changed")
	}
	s := string(out)
	if !strings.Contains(s, "filerwire.NewLookupRequest(filerwire.LookupRequestInput{Name: e.Name})") {
		t.Errorf("construction not rewritten:\n%s", s)
	}
	// *filer_pb.Entry param still references filer_pb -> import stays, pbLeft>0.
	if !strings.Contains(s, "filer_pb.Entry") {
		t.Errorf("expected filer_pb.Entry type ref to remain (human owns it):\n%s", s)
	}
	if pbLeft == 0 {
		t.Errorf("pbLeft = 0, want > 0 (the *filer_pb.Entry param)")
	}
}

package v5

import "testing"

func TestDeFraggerLRUSizeCap(t *testing.T) {
	var d deFragger
	for id := uint16(0); id < 256; id++ {
		p := &Packet{PKT_ID: id, FRAG_TOTAL: 2, FRAG_ID: 0, DATA: []byte{1}}
		if got := d.Feed(p); got != nil {
			t.Fatalf("incomplete fragment for pkt %d assembled", id)
		}
	}
	if got := d.Feed(&Packet{PKT_ID: 256, FRAG_TOTAL: 2, FRAG_ID: 0, DATA: []byte{1}}); got != nil {
		t.Fatal("incomplete 257th fragment assembled")
	}
	if got := d.Feed(&Packet{PKT_ID: 0, FRAG_TOTAL: 2, FRAG_ID: 1, DATA: []byte{2}}); got != nil {
		t.Fatal("evicted packet assembled after size cap")
	}
}

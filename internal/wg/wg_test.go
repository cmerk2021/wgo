package wg

import (
	"testing"
	"time"
)

const dump = "cHJpdmF0ZQ==\tcHVibGlj\t51820\toff\n" +
	"cGVlcmtleQ==\t(none)\t203.0.113.5:51820\t0.0.0.0/0,::/0\t1700000000\t123456\t654321\t25\n"

func TestParseDump(t *testing.T) {
	ds, err := ParseDump("wg0", dump)
	if err != nil {
		t.Fatal(err)
	}
	if ds.ListenPort != 51820 {
		t.Errorf("listen port: %d", ds.ListenPort)
	}
	if len(ds.Peers) != 1 {
		t.Fatalf("peers: %+v", ds.Peers)
	}
	p := ds.Peers[0]
	if p.Endpoint != "203.0.113.5:51820" {
		t.Errorf("endpoint: %s", p.Endpoint)
	}
	if p.RxBytes != 123456 || p.TxBytes != 654321 {
		t.Errorf("bytes: %d %d", p.RxBytes, p.TxBytes)
	}
	if !p.LastHandshake.Equal(time.Unix(1700000000, 0)) {
		t.Errorf("handshake: %v", p.LastHandshake)
	}
	if ds.TotalRx() != 123456 || ds.TotalTx() != 654321 {
		t.Errorf("totals: %d %d", ds.TotalRx(), ds.TotalTx())
	}
	if ds.Endpoint() != "203.0.113.5:51820" {
		t.Errorf("device endpoint: %s", ds.Endpoint())
	}
}

func TestParseDumpZeroHandshake(t *testing.T) {
	d := "a\tb\t0\toff\n" + "peer\t(none)\t(none)\t(none)\t0\t0\t0\toff\n"
	ds, err := ParseDump("wg0", d)
	if err != nil {
		t.Fatal(err)
	}
	if !ds.LatestHandshake().IsZero() {
		t.Error("zero handshake must stay zero")
	}
	if ds.Peers[0].Endpoint != "" {
		t.Error("(none) endpoint must be empty")
	}
}

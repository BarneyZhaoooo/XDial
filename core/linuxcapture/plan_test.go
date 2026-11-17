package linuxcapture

import "testing"

func TestPlanReservations(t *testing.T) {
	p := Plan{CaptureMark: 1 << 16, BypassMark: 1 << 17, Table: 50000, Priority: 32000, Listener: "127.0.0.1:15080"}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*Plan){
		func(p *Plan) { p.CaptureMark = 0 }, func(p *Plan) { p.BypassMark = p.CaptureMark }, func(p *Plan) { p.CaptureMark = 3 },
		func(p *Plan) { p.Table = 254 }, func(p *Plan) { p.Priority = 0 }, func(p *Plan) { p.Priority = 32766 },
		func(p *Plan) { p.Listener = "localhost:15080" }, func(p *Plan) { p.Listener = "0.0.0.0:15080" }, func(p *Plan) { p.Listener = "[::1]:15080" }, func(p *Plan) { p.Listener = "127.0.0.1:0" },
	} {
		q := p
		change(&q)
		if q.Validate() == nil {
			t.Fatal("accepted unsafe reservation", q)
		}
	}
}

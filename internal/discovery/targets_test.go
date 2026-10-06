package discovery

import "testing"

func TestBroadcastTargets(t *testing.T) {
	for _, tg := range broadcastTargets() {
		if tg.local.To4() == nil || tg.bcast.To4() == nil {
			t.Errorf("non-IPv4 target: %v", tg)
		}
		t.Logf("local %s broadcast %s", tg.local, tg.bcast)
	}
}

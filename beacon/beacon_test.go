package beacon

import (
	"context"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// recorded are real quicknet rounds read from api.drand.sh on 2026-11-02.
var recorded = []struct {
	number                uint64
	signature, randomness string
}{
	{33620529, "8c86700b0b3ecd2c45aa44a19feeb796914a58a7e0d6fefaad6364520787162a22f89a9fe87f132c9e7dc10ff389e1d7", "b174b768d659aad91d60b6cfc867e54ec7659ca0ce13c91c92359892f0e364ef"},
	{33620530, "a60689e1fde2cebdcc07afac0cb2e664904b0aab8c3aeaf27ad1795349b91e013ee44856ca5bee2db4dd04abc2e4496a", "9d4af48b009579b350d040d0b6d0c77f5c0d7baf9225997b452359c3820e015d"},
	{1000000, "83ad29e4c409f9470fc2ef02f90214df49e02b441a1a241a82d622d9f608ef98fd8b11a029f1bee9d9e83b45088abe72", "b22aad4794f7451896f7a371aa46106fd84d919f3f569acd5b2fddf1d1440af3"},
}

func TestRecordedRoundsVerifyAgainstThePinnedKey(t *testing.T) {
	for _, r := range recorded {
		got, err := Verify(r.number, r.signature)
		if err != nil || hex.EncodeToString(got) != r.randomness {
			t.Errorf("round %d: %x, %v; want %s", r.number, got, err, r.randomness)
		}
		// One altered byte of the signature fails.
		bad := []byte(r.signature)
		if bad[10] == '0' {
			bad[10] = '1'
		} else {
			bad[10] = '0'
		}
		if _, err := Verify(r.number, string(bad)); err == nil {
			t.Errorf("round %d verified with an altered signature", r.number)
		}
		// The signature of one round is not another round's.
		if _, err := Verify(r.number+1, r.signature); err == nil {
			t.Errorf("round %d's signature verified as round %d", r.number, r.number+1)
		}
	}
}

func TestRoundAtIsTheFirstRoundAtOrAfterAMoment(t *testing.T) {
	g := time.Unix(Genesis, 0)
	for _, c := range []struct {
		at   time.Time
		want uint64
	}{
		{g.Add(-time.Hour), 1}, {g, 1}, {g.Add(time.Nanosecond), 2}, {g.Add(3 * time.Second), 2}, {g.Add(3*time.Second + time.Millisecond), 3},
	} {
		if got := RoundAt(c.at); got != c.want {
			t.Errorf("RoundAt(genesis%+v) = %d, want %d", c.at.Sub(g), got, c.want)
		}
	}
	for _, n := range []uint64{1, 2, 33620529} {
		if RoundAt(TimeOf(n)) != n || RoundAt(TimeOf(n).Add(time.Second)) != n+1 {
			t.Errorf("round %d does not map back to its moment", n)
		}
	}
	// 19:05 New York on November 2, 2026 is round 33,625,847's moment or just before it.
	ny, _ := time.LoadLocation("America/New_York")
	due := time.Date(2026, 11, 2, 19, 5, 0, 0, ny)
	if n := RoundAt(due); TimeOf(n).Before(due) || TimeOf(n).Sub(due) >= Period {
		t.Errorf("RoundAt(%s) = %d at %s", due, n, TimeOf(n))
	}
}

// relay serves recorded rounds as a drand relay does, or fails.
func relay(t *testing.T, behave string) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if behave == "down" {
			http.Error(w, "down", http.StatusBadGateway)
			return
		}
		for _, rec := range recorded {
			if r.URL.Path == fmt.Sprintf("/%s/public/%d", ChainHash, rec.number) {
				sig := rec.signature
				if behave == "forged" {
					sig = strings.Repeat("ab", len(sig)/2)
				}
				fmt.Fprintf(w, `{"round":%d,"randomness":"%s","signature":"%s"}`, rec.number, rec.randomness, sig)
				return
			}
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(ts.Close)
	return ts
}

func TestTheClientMovesToTheNextRelayAndVerifiesWhatItGets(t *testing.T) {
	down, forged, good := relay(t, "down"), relay(t, "forged"), relay(t, "good")
	now := time.Now()
	c := &Client{Relays: []string{down.URL, forged.URL, good.URL}, HTTP: http.DefaultClient, Now: func() time.Time { return now }}
	r, err := c.Round(context.Background(), recorded[0].number)
	if err != nil || r.Randomness != recorded[0].randomness || r.Signature != recorded[0].signature {
		t.Fatalf("round from the third relay: %+v, %v", r, err)
	}
	c.Relays = []string{down.URL, forged.URL}
	if _, err := c.Round(context.Background(), recorded[0].number); err == nil || !strings.Contains(err.Error(), "no relay served a valid round") {
		t.Errorf("no good relay: %v", err)
	}
	// A round whose moment has not come is not asked for.
	c.Now = func() time.Time { return TimeOf(recorded[0].number).Add(-time.Second) }
	if _, err := c.Round(context.Background(), recorded[0].number); err == nil {
		t.Error("a future round was served")
	} else if _, ok := err.(NotYetError); !ok {
		t.Errorf("a future round: %v, want a NotYetError", err)
	}
}

func TestMudBeaconRelaysReplacesTheDefaultList(t *testing.T) {
	t.Setenv("MUD_BEACON_RELAYS", "")
	if c := New(); strings.Join(c.Relays, ",") != strings.Join(DefaultRelays, ",") || len(c.Relays) != 4 {
		t.Errorf("default relays %v", c.Relays)
	}
	t.Setenv("MUD_BEACON_RELAYS", " https://a.example/ , https://b.example ")
	if c := New(); strings.Join(c.Relays, ",") != "https://a.example,https://b.example" {
		t.Errorf("relays from MUD_BEACON_RELAYS %v", c.Relays)
	}
}

func TestTheStubVerifiesItsOwnRoundsOnly(t *testing.T) {
	var s Stub
	r, err := s.Round(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := s.Verify(7, r.Signature); err != nil || hex.EncodeToString(got) != r.Randomness {
		t.Errorf("stub verify: %x %v", got, err)
	}
	if _, err := s.Verify(8, r.Signature); err == nil {
		t.Error("a stub signature verified for another round")
	}
	if _, err := (Stub{Missing: 7}).Round(context.Background(), 7); err == nil {
		t.Error("a missing round was served")
	}
}

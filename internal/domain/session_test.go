package domain

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

// The JSON tags on IncomingSession are the contract with the SDK, and they fail
// silently: encoding/json ignores a key it has no field for, so a mismatched
// tag produces a session that arrives complete and blank in that column. This
// is the payload the SDK actually sends, byte for byte.
func TestIncomingSessionReadsWhatTheSDKSends(t *testing.T) {
	const payload = `[{
		"id": "4f2a81b0-0000-4000-8000-000000000001",
		"installation_id": "e7c1b8a2-40aa-4f12-9c07-3d5581bb40aa",
		"user_id": "u_8812",
		"device_model": "Pixel 7",
		"os_name": "Android",
		"os_version": "14",
		"app_version": "3.11.2",
		"build_number": "418",
		"metadata": {"plan": "free"},
		"started_at": "2026-07-30T14:16:02.000Z",
		"last_seen_at": "2026-07-30T14:22:43.000Z",
		"sdk_version": "1.3.1"
	}]`

	var sessions []IncomingSession
	if err := json.Unmarshal([]byte(payload), &sessions); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1", len(sessions))
	}

	in := sessions[0]
	for _, f := range []struct {
		name string
		got  *string
		want string
	}{
		{"user_id", in.UserID, "u_8812"},
		{"device_model", in.DeviceModel, "Pixel 7"},
		{"os_name", in.OSName, "Android"},
		{"os_version", in.OSVersion, "14"},
		{"app_version", in.AppVersion, "3.11.2"},
		{"build_number", in.BuildNumber, "418"},
		{"sdk_version", in.SDKVersion, "1.3.1"},
	} {
		if f.got == nil {
			t.Errorf("%s did not decode, so its JSON tag does not match the SDK", f.name)
			continue
		}
		if *f.got != f.want {
			t.Errorf("%s = %q, want %q", f.name, *f.got, f.want)
		}
	}
}

// An SDK from before the field existed sends no such key, and that is a null
// column rather than an error. Not a compatibility workaround: it is what a
// nullable column means, and the published SDK is the one wire contract this
// project does have to honour.
func TestIncomingSessionWithoutAnSDKVersion(t *testing.T) {
	const payload = `[{"id":"4f2a81b0-0000-4000-8000-000000000001"}]`

	var sessions []IncomingSession
	if err := json.Unmarshal([]byte(payload), &sessions); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if sessions[0].SDKVersion != nil {
		t.Errorf("SDKVersion = %q, want nil", *sessions[0].SDKVersion)
	}

	// And it survives the mapping into the stored entity rather than being
	// dropped somewhere between the two.
	s := sessions[0].ToSession(uuid.New(), time.Now())
	if s.SDKVersion != nil {
		t.Errorf("Session.SDKVersion = %q, want nil", *s.SDKVersion)
	}
}

// ToSession is a hand-written mapper, which is the kind that silently forgets a
// field. The upload path is Incoming -> Session -> model, and a field lost at
// the first hop looks exactly like an SDK that never sent it.
func TestToSessionCarriesTheSDKVersion(t *testing.T) {
	v := "1.4.0"
	in := IncomingSession{ID: uuid.New(), SDKVersion: &v}

	s := in.ToSession(uuid.New(), time.Now())
	if s.SDKVersion == nil {
		t.Fatal("ToSession dropped sdk_version")
	}
	if *s.SDKVersion != v {
		t.Errorf("sdk_version = %q, want %q", *s.SDKVersion, v)
	}
}

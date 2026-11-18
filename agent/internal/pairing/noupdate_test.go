package pairing

import (
	"context"
	"testing"
	"time"
)

// R-131 (#35 review): a phone kept from updating the agent could pair
// itself again under another name, answer its own request and get a token
// that may update. A device such a phone lets in is kept from it too; one
// the PC itself lets in, or a phone that may update, is not.
func TestAPhoneKeptFromUpdatingLetsInOnlyDevicesKeptFromIt(t *testing.T) {
	for _, c := range []struct {
		name     string
		noUpdate bool
	}{{"kept from updating", true}, {"may update", false}} {
		p, _ := newPairer(t)
		if _, _, err := p.Devices.Add("Old phone"); err != nil {
			t.Fatal(err)
		}
		p.Confirm = func(ctx context.Context, _ Question) (bool, bool) {
			<-ctx.Done()
			return false, true
		}
		id, err := p.StartDirect("Another name", "08", phone, nil)
		if err != nil {
			t.Fatal(err)
		}
		var q Question
		deadline := time.Now().Add(2 * time.Second)
		for {
			if list := p.Pending(); len(list) == 1 {
				q = list[0]
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("the request never showed up for the paired phone")
			}
			time.Sleep(5 * time.Millisecond)
		}
		if err := p.AnswerAs(q.ID, true, c.noUpdate); err != nil {
			t.Fatal(err)
		}
		st, tok, d := waitFinal(t, p, id)
		if st != StateApproved || tok == "" {
			t.Fatalf("%s: state %s", c.name, st)
		}
		got, ok := p.Devices.Get(d.ID)
		if !ok || got.NoUpdate != c.noUpdate {
			t.Fatalf("%s: the new device has NoUpdate=%v", c.name, got.NoUpdate)
		}
	}
}

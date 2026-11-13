// SPDX-License-Identifier: Apache-2.0

package alerts_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/alerts"
	"github.com/aazam476/sentinel/internal/testutil/fakeclock"
)

func updates(total, security int) protocol.PackageUpdatesPayload {
	list := []protocol.PackageUpdate{}
	for index := range total {
		list = append(list, protocol.PackageUpdate{ID: fmt.Sprint("p", index), Security: index < security})
	}
	return protocol.PackageUpdatesPayload{Sources: []protocol.PackageSource{{Manager: "apt", Updates: list}}}
}

// sendUpdatesTo routes the updates rule to a webhook.
func sendUpdatesTo(t *testing.T, h *harness) {
	t.Helper()
	channel := saveWebhook(t, h)
	h.exec(t, `INSERT INTO alert_rule_channels (rule_id, channel_id) SELECT id, ? FROM alert_rules WHERE type = 'updates'`, channel.ID)
}

func TestUpdatesCondition(t *testing.T) {
	cases := []struct {
		name            string
		total, security int
		want            string
	}{
		{name: "none", want: ""},
		{name: "one update", total: 1, want: "1 update available."},
		{name: "many, some security fixes", total: 12, security: 3, want: "12 updates available, 3 of them security fixes."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			condition := alerts.UpdatesCondition(updates(c.total, c.security))
			got := ""
			if condition != nil {
				got = condition.Message
			}
			if got != c.want {
				t.Errorf("message = %q, want %q", got, c.want)
			}
		})
	}
}

func TestRecordPackageUpdates(t *testing.T) {
	t.Run("raises an alert without notifying, and resolves it once updated", func(t *testing.T) {
		h := newHarness(t)
		sendUpdatesTo(t, h)
		if changed, err := h.service.RecordPackageUpdates(t.Context(), h.agentID, updates(4, 1)); err != nil || !changed {
			t.Fatalf("changed %v, %v", changed, err)
		}
		h.service.Wait()
		if got := states(t, h); !slices.Equal(got, []string{"firing updates.available "}) {
			t.Errorf("got %v", got)
		}
		if sent := h.receiver.take(); len(sent) != 0 || !slices.Equal(h.changed, []string{nasID}) {
			t.Errorf("sent %v, changed %v", sent, h.changed)
		}
		if changed, err := h.service.RecordPackageUpdates(t.Context(), h.agentID, updates(0, 0)); err != nil || !changed {
			t.Fatalf("changed %v, %v", changed, err)
		}
		h.service.Wait()
		if got := states(t, h); !slices.Equal(got, []string{"resolved updates.available "}) || len(h.receiver.take()) != 0 {
			t.Errorf("got %v", got)
		}
	})

	t.Run("raises nothing once the rule is switched off, or for a machine it doesn't know", func(t *testing.T) {
		h := newHarness(t)
		if changed, err := h.service.RecordPackageUpdates(t.Context(), 9999, updates(1, 0)); err != nil || changed {
			t.Errorf("unknown machine: changed %v, %v", changed, err)
		}
		h.exec(t, `UPDATE alert_rules SET enabled = 0 WHERE type = 'updates'`)
		if changed, err := h.service.RecordPackageUpdates(t.Context(), h.agentID, updates(1, 0)); err != nil || changed {
			t.Errorf("changed %v, %v", changed, err)
		}
	})

	t.Run("reports a database failure", func(t *testing.T) {
		service := alerts.NewService(alerts.Options{Database: failingDatabase{}, Clock: fakeclock.New(time.Now())})
		if _, err := service.RecordPackageUpdates(t.Context(), 1, updates(1, 0)); err == nil {
			t.Error("err = nil")
		}
		if err := service.SendUpdatesDigest(t.Context()); err == nil {
			t.Error("digest err = nil")
		}
	})
}

func TestSendUpdatesDigest(t *testing.T) {
	t.Run("sends one notification naming every machine with updates, in order", func(t *testing.T) {
		h := newHarness(t)
		sendUpdatesTo(t, h)
		web := h.addMachine(t, "01JBX3K5Q7Z8M4N6P8R0T2V4W1", "web")
		quiet := h.addMachine(t, "01JBX3K5Q7Z8M4N6P8R0T2V4W2", "quiet")
		for agentID, list := range map[int64]protocol.PackageUpdatesPayload{h.agentID: updates(2, 0), web: updates(12, 3), quiet: updates(0, 0)} {
			if _, err := h.service.RecordPackageUpdates(t.Context(), agentID, list); err != nil {
				t.Fatal(err)
			}
		}
		if err := h.service.SendUpdatesDigest(t.Context()); err != nil {
			t.Fatal(err)
		}
		h.service.Wait()
		sent := h.receiver.take()
		if len(sent) != 1 {
			t.Fatalf("sent %d notifications", len(sent))
		}
		body := sent[0].JSON(t)
		want := "Updates are waiting on 2 machines. nas: 2 updates available; web: 12 updates available, 3 of them security fixes."
		if body["message"] != want || body["machine"] != "2 machines" || body["rule"] != "Updates available" {
			t.Errorf("body = %v", body)
		}
	})

	t.Run("names one machine as itself, and leaves out machines in maintenance", func(t *testing.T) {
		h := newHarness(t)
		sendUpdatesTo(t, h)
		resting := h.addMachine(t, "01JBX3K5Q7Z8M4N6P8R0T2V4W1", "resting")
		h.exec(t, `UPDATE agents SET maintenance_until = ? WHERE id = ?`, h.clock.Now().Add(time.Hour).UnixMilli(), resting)
		for _, agentID := range []int64{h.agentID, resting} {
			if _, err := h.service.RecordPackageUpdates(t.Context(), agentID, updates(1, 0)); err != nil {
				t.Fatal(err)
			}
		}
		if err := h.service.SendUpdatesDigest(t.Context()); err != nil {
			t.Fatal(err)
		}
		h.service.Wait()
		sent := h.receiver.take()
		if len(sent) != 1 || sent[0].JSON(t)["machine"] != "nas" || sent[0].JSON(t)["message"] != "1 update available." {
			t.Errorf("sent %v", sent)
		}
	})

	t.Run("counts the machines past the twentieth, and sends nothing when none have updates", func(t *testing.T) {
		h := newHarness(t)
		sendUpdatesTo(t, h)
		if err := h.service.SendUpdatesDigest(t.Context()); err != nil {
			t.Fatal(err)
		}
		h.service.Wait()
		if sent := h.receiver.take(); len(sent) != 0 {
			t.Errorf("sent %v with no updates anywhere", sent)
		}
		for index := range 22 {
			agentID := h.addMachine(t, fmt.Sprintf("01JBX3K5Q7Z8M4N6P8R0T2V5%02d", index), fmt.Sprintf("host-%02d", index))
			if _, err := h.service.RecordPackageUpdates(t.Context(), agentID, updates(1, 0)); err != nil {
				t.Fatal(err)
			}
		}
		if err := h.service.SendUpdatesDigest(t.Context()); err != nil {
			t.Fatal(err)
		}
		h.service.Wait()
		sent := h.receiver.take()
		if len(sent) != 1 || !strings.HasSuffix(sent[0].JSON(t)["message"].(string), "; and 2 more machines.") {
			t.Errorf("sent %v", sent)
		}
	})
}

// fakeMachines answers package.list.updates for some machines and fails
// for the rest.
type fakeMachines struct {
	answers map[int64]protocol.PackageUpdatesPayload
	asked   chan string
}

func (f fakeMachines) Online() []int64 { return []int64{1, 2, 3} }

func (f fakeMachines) RequestWithin(_ context.Context, agentID int64, timeout time.Duration, action string, _, result any) error {
	f.asked <- fmt.Sprintf("%d %s %s", agentID, action, timeout)
	list, ok := f.answers[agentID]
	if !ok {
		return errors.New("this machine has no package manager Sentinel supports")
	}
	*result.(*protocol.PackageUpdatesPayload) = list
	return nil
}

func TestCheckPackages(t *testing.T) {
	t.Run("asks every online machine, records what each has, and sends the digest", func(t *testing.T) {
		h := newHarness(t)
		sendUpdatesTo(t, h)
		web := h.addMachine(t, "01JBX3K5Q7Z8M4N6P8R0T2V4W1", "web")
		machines := fakeMachines{answers: map[int64]protocol.PackageUpdatesPayload{h.agentID: updates(3, 1), web: updates(0, 0)}, asked: make(chan string, 3)}
		if err := h.service.CheckPackages(t.Context(), machines); err != nil {
			t.Fatal(err)
		}
		h.service.Wait()
		close(machines.asked)
		var asked []string
		for line := range machines.asked {
			asked = append(asked, line)
		}
		slices.Sort(asked)
		if !slices.Equal(asked, []string{"1 package.list.updates 10m0s", "2 package.list.updates 10m0s", "3 package.list.updates 10m0s"}) {
			t.Errorf("asked %v", asked)
		}
		sent := h.receiver.take()
		if len(sent) != 1 || sent[0].JSON(t)["message"] != "3 updates available, 1 of them security fixes." {
			t.Errorf("sent %v", sent)
		}
	})

	t.Run("reports a digest that couldn't be read", func(t *testing.T) {
		service := alerts.NewService(alerts.Options{Database: failingDatabase{}, Clock: fakeclock.New(time.Now())})
		machines := fakeMachines{answers: map[int64]protocol.PackageUpdatesPayload{1: updates(1, 0)}, asked: make(chan string, 3)}
		if err := service.CheckPackages(t.Context(), machines); err == nil {
			t.Error("err = nil")
		}
	})
}

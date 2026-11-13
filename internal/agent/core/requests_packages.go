// SPDX-License-Identifier: Apache-2.0

package core

import (
	"context"
	"errors"
	"time"

	"github.com/aazam476/sentinel/internal/agent/packages"
	"github.com/aazam476/sentinel/internal/agent/policy"
	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/shared/clock"
)

// noPackageManager is why the packages capability is off on a machine
// without a package manager Sentinel knows.
const noPackageManager = "This machine has no package manager Sentinel supports"

// applyLimit bounds an install. It isn't tied to the connection: stopping
// a package manager halfway can leave the machine broken.
const applyLimit = 2 * time.Hour

// capabilities is the local policy's report, with packages switched off
// when the machine has no package manager: the UI then shows no Updates
// tab rather than one that always fails.
func (a *Agent) capabilities() []protocol.Capability {
	report := a.options.Policy.Report()
	for index := range report {
		if report[index].Name == policy.Packages && report[index].Enabled && !a.options.Packages.Supported() {
			report[index].Enabled, report[index].Reason = false, noPackageManager
		}
	}
	return report
}

// listPackageUpdates lists what the machine's package managers can update.
func (c *connection) listPackageUpdates(ctx context.Context, _ protocol.Message) (any, *protocol.Error) {
	if refusal := c.refuseUnlessAllowed(policy.Packages, "Package updates"); refusal != nil {
		return nil, refusal
	}
	updates, err := c.agent.options.Packages.List(ctx)
	if err != nil {
		return nil, c.packagesError(err, "list the updates")
	}
	return updates, nil
}

// applyPackageUpdates installs updates. A manager that fails still
// answers with what it printed, so people can see why.
func (c *connection) applyPackageUpdates(ctx context.Context, request protocol.Message) (any, *protocol.Error) {
	if refusal := c.refuseUnlessAllowed(policy.Packages, "Package updates"); refusal != nil {
		return nil, refusal
	}
	var payload protocol.PackageApplyPayload
	if err := protocol.DecodePayload(request, &payload); err != nil {
		return nil, invalidRequest(err)
	}
	c.agent.options.Logger.Warn("installing updates at the server's request", "manager", payload.Manager, "updates", len(payload.IDs))
	ctx, cancel := clock.WithTimeout(context.WithoutCancel(ctx), c.agent.options.Clock, applyLimit)
	defer cancel()
	result, err := c.agent.options.Packages.Apply(ctx, payload)
	if err != nil {
		refusal := c.packagesError(err, "install the updates")
		if refusal.Code == protocol.ErrorInternal && result.Output != "" {
			refusal.Message += "\n\n" + result.Output
		}
		return nil, refusal
	}
	return result, nil
}

func (c *connection) packagesError(err error, doing string) *protocol.Error {
	switch {
	case errors.Is(err, packages.ErrInvalidID), errors.Is(err, packages.ErrNotSelectable):
		return &protocol.Error{Code: protocol.ErrorInvalidRequest, Message: err.Error()}
	case errors.Is(err, packages.ErrUnsupported), errors.Is(err, packages.ErrUnknownManager):
		return &protocol.Error{Code: protocol.ErrorUnsupportedPlatform, Message: err.Error()}
	default:
		c.agent.options.Logger.Warn("a package request failed", "doing", doing, "error", err)
		return &protocol.Error{Code: protocol.ErrorInternal, Message: "The agent couldn't " + doing + ": " + err.Error()}
	}
}

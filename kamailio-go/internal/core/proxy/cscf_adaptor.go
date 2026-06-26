// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * CSCFAdaptor — the attachment surface for IMS P/I/S-CSCF role handlers
 * on ProxyCore's dispatch path. Each role implements this interface;
 * the bootstrap attaches the relevant adaptors based on --role.
 */

package proxy

import (
	"context"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// CSCF role identifiers.
const (
	RolePCSCF = iota
	RoleICSCF
	RoleSCSCF
)

// CSCFAdaptor is a CSCF role's hook into ProxyCore's dispatch path.
// Implementations call their IMS business layer, then return a
// ResponseAction that either:
//   - responds directly (Status/Reason/ExtraHeaders set, StopRouting=true)
//     for terminal roles (e.g. S-CSCF REGISTER); or
//   - forwards to a next hop (Target set, StopRouting=true) for transit
//     roles (e.g. P-CSCF forwarding REGISTER to I-CSCF); or
//   - declines (zero value) so the next adaptor / fallback registrar runs.
type CSCFAdaptor interface {
	Role() int
	HandleRegister(ctx context.Context, msg *parser.SIPMsg) ResponseAction
	HandleInvite(ctx context.Context, msg *parser.SIPMsg) ResponseAction
	HandleInDialog(ctx context.Context, msg *parser.SIPMsg) ResponseAction
}

// applyCSCFAction reports whether the adaptor produced a terminal action
// (Status != 0, Target != "", or StopRouting). A declined adaptor returns
// the zero value and applyCSCFAction returns false so dispatch can continue.
func applyCSCFAction(_ ResponseAction, act ResponseAction) bool {
	return act.Status != 0 || act.Target != "" || act.StopRouting
}

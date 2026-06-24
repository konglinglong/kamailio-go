// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - Extended action types matching C route_struct.h
 *
 * This file adds the remaining action types from Kamailio's C
 * route_struct.h that are not in the original script.go.
 * The executor in action_exec.go handles dispatch.
 */

package script

// ExtendedActionType enumerates additional action types from C.
// These extend the base ActionType constants in script.go.
const (
	// URI manipulation actions (C: SET_HOST_T, SET_USER_T, etc.)
	ActSetHost ActionType = iota + 100
	ActSetHostPort
	ActSetUser
	ActSetUserPass
	ActSetPort
	ActSetHostPortTrans
	ActSetHostAll
	ActSetUserPhone
	ActRevertURI

	// Prefix/stripping actions
	ActPrefix
	ActStrip
	ActStripTail

	// Branch manipulation
	ActRemoveBranch
	ActClearBranches

	// Transport-specific forward
	ActForwardTCP
	ActForwardUDP
	ActForwardTLS
	ActForwardSCTP

	// Network manipulation
	ActForceRPort
	ActAddLocalRPort
	ActSetAdvAddr
	ActSetAdvPort
	ActForceSendSocket
	ActForceTCPAlias

	// Flag operations
	ActIsFlagSet

	// Connection control
	ActSetFwdNoConnect
	ActSetRplNoConnect
	ActSetFwdClose
	ActSetRplClose

	// MTU
	ActUDPMTUTryProto

	// Control flow
	ActWhile
	ActSwitch
	ActBreak
	ActExit

	// Module function call
	ActModule0
	ActModule1
	ActModule2
	ActModule3
	ActModuleX

	// AVP operations
	ActAVPToURI
	ActLoadAVP

	// Config operations
	ActCfgSelect
	ActCfgReset

	// Error handling
	ActError

	// Misc
	ActEval
	ActAssign
	ActAdd

	// Len comparison
	ActLenGT
)

// RunFlags for action execution control
const (
	ExitRF    = 1 << 0 // EXIT_R_F - stop script execution
	ReturnRF  = 1 << 1 // RETURN_R_F - return from current route
	BreakRF   = 1 << 2 // BREAK_R_F - break from switch/while
	DropRF    = 1 << 3 // DROP_R_F - drop the message
)

// RunActCtx is the runtime action context, matching C run_act_ctx_t.
type RunActCtx struct {
	RecLev     int
	RunFlags   int
	LastRetCode int
}

// InitRunActCtx initializes a runtime action context.
func InitRunActCtx(ctx *RunActCtx) {
	ctx.RecLev = 0
	ctx.RunFlags = 0
	ctx.LastRetCode = 0
}

// MaxRecursiveLevel limits nested route calls.
const MaxRecursiveLevel = 256

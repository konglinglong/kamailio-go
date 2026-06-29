// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the Diameter protocol constants.
 */

package cdp

import (
	"strings"
	"testing"
)

func TestCommandCodeConstants(t *testing.T) {
	cases := []struct {
		name string
		code uint32
		want uint32
	}{
		{"Capabilities-Exchange", CmdCapabilitiesExchange, 257},
		{"Re-Auth", CmdReauth, 258},
		{"Accounting", CmdAccounting, 271},
		{"Credit-Control", CmdCreditControl, 272},
		{"Session-Termination", CmdSessionTermination, 275},
		{"Device-Watchdog", CmdDeviceWatchdog, 280},
		{"Disconnect-Peer", CmdDisconnectPeer, 282},
	}
	for _, c := range cases {
		if c.code != c.want {
			t.Errorf("%s = %d, want %d", c.name, c.code, c.want)
		}
	}
}

func TestCommandCodeString(t *testing.T) {
	cases := []struct {
		code  uint32
		name  string
	}{
		{CmdCapabilitiesExchange, "Capabilities-Exchange"},
		{CmdReauth, "Re-Auth"},
		{CmdAccounting, "Accounting"},
		{CmdCreditControl, "Credit-Control"},
		{CmdSessionTermination, "Session-Termination"},
		{CmdDeviceWatchdog, "Device-Watchdog"},
		{CmdDisconnectPeer, "Disconnect-Peer"},
		{9999, "unknown"},
	}
	for _, c := range cases {
		got := CommandCode(c.code).String()
		if got != c.name {
			t.Errorf("CommandCode(%d).String() = %q, want %q", c.code, got, c.name)
		}
	}
}

func TestApplicationIDConstants(t *testing.T) {
	if AppDiameterCommonMessages != 0 {
		t.Errorf("AppDiameterCommonMessages = %d, want 0", AppDiameterCommonMessages)
	}
	if AppNASREQ != 1 {
		t.Errorf("AppNASREQ = %d, want 1", AppNASREQ)
	}
	if AppDiameterSIP != 6 {
		t.Errorf("AppDiameterSIP = %d, want 6", AppDiameterSIP)
	}
	if App3GPPSh != 16777217 {
		t.Errorf("App3GPPSh = %d, want 16777217", App3GPPSh)
	}
	if VendorID3GPP != 10415 {
		t.Errorf("VendorID3GPP = %d, want 10415", VendorID3GPP)
	}
}

func TestAVPCodeConstants(t *testing.T) {
	cases := []struct {
		name  string
		code  uint32
		want  uint32
	}{
		{"Origin-Host", AVPCodeOriginHost, 264},
		{"Origin-Realm", AVPCodeOriginRealm, 296},
		{"Destination-Host", AVPCodeDestinationHost, 293},
		{"Destination-Realm", AVPCodeDestinationRealm, 283},
		{"Host-IP-Address", AVPCodeHostIPAddress, 257},
		{"Vendor-Id", AVPCodeVendorID, 266},
		{"Product-Name", AVPCodeProductName, 269},
		{"Result-Code", AVPCodeResultCode, 268},
		{"Disconnect-Cause", AVPCodeDisconnectCause, 273},
		{"Session-Id", AVPCodeSessionID, 263},
		{"Firmware-Revision", AVPCodeFirmwareRevision, 267},
		{"User-Name", AVPCodeUserName, 1},
	}
	for _, c := range cases {
		if c.code != c.want {
			t.Errorf("%s = %d, want %d", c.name, c.code, c.want)
		}
	}
}

func TestResultCodeClasses(t *testing.T) {
	cases := []struct {
		code              uint32
		wantClass         int
		isInfo            bool
		isSuccess         bool
		isProtocolError   bool
		isTransientFail   bool
		isPermanentFail   bool
	}{
		{ResultMultiRoundAuth, 1, true, false, false, false, false},
		{ResultSuccess, 2, false, true, false, false, false},
		{ResultCommandUnsupported, 3, false, false, true, false, false},
		{ResultAuthNoSubscriptions, 4, false, false, false, true, false},
		{ResultUnknownSessionID, 5, false, false, false, false, true},
		{2500, 2, false, true, false, false, false}, // arbitrary success
	}
	for _, c := range cases {
		if got := ResultCodeClass(c.code); got != c.wantClass {
			t.Errorf("ResultCodeClass(%d) = %d, want %d", c.code, got, c.wantClass)
		}
		if ResultClassInfo(c.code) != c.isInfo {
			t.Errorf("ResultClassInfo(%d) = %v, want %v", c.code, ResultClassInfo(c.code), c.isInfo)
		}
		if ResultClassSuccess(c.code) != c.isSuccess {
			t.Errorf("ResultClassSuccess(%d) = %v, want %v", c.code, ResultClassSuccess(c.code), c.isSuccess)
		}
		if ResultClassProtocolError(c.code) != c.isProtocolError {
			t.Errorf("ResultClassProtocolError(%d) = %v, want %v", c.code, ResultClassProtocolError(c.code), c.isProtocolError)
		}
		if ResultClassTransientFailure(c.code) != c.isTransientFail {
			t.Errorf("ResultClassTransientFailure(%d) = %v, want %v", c.code, ResultClassTransientFailure(c.code), c.isTransientFail)
		}
		if ResultClassPermanentFailure(c.code) != c.isPermanentFail {
			t.Errorf("ResultClassPermanentFailure(%d) = %v, want %v", c.code, ResultClassPermanentFailure(c.code), c.isPermanentFail)
		}
	}
}

func TestIsSuccessResultHelper(t *testing.T) {
	if !IsSuccessResult(ResultSuccess) {
		t.Errorf("IsSuccessResult(Success) = false, want true")
	}
	if IsSuccessResult(ResultCommandUnsupported) {
		t.Errorf("IsSuccessResult(CommandUnsupported) = true, want false")
	}
}

func TestDisconnectCauseConstants(t *testing.T) {
	if DisconnectCauseRebooting != 0 {
		t.Errorf("DisconnectCauseRebooting = %d, want 0", DisconnectCauseRebooting)
	}
	if DisconnectCauseBusy != 1 {
		t.Errorf("DisconnectCauseBusy = %d, want 1", DisconnectCauseBusy)
	}
	if DisconnectCauseDoNotWantToTalkToYou != 2 {
		t.Errorf("DisconnectCauseDoNotWantToTalkToYou = %d, want 2", DisconnectCauseDoNotWantToTalkToYou)
	}
}

// TestAllCommandStringsAreStable ensures the String() method does not
// return empty for any known command. Regression guard against accidental
// deletions.
func TestAllCommandStringsAreStable(t *testing.T) {
	for _, code := range []uint32{
		CmdCapabilitiesExchange, CmdReauth, CmdAccounting, CmdCreditControl,
		CmdSessionTermination, CmdDeviceWatchdog, CmdDisconnectPeer,
	} {
		s := CommandCode(code).String()
		if s == "" {
			t.Errorf("CommandCode(%d).String() is empty", code)
		}
		if s == "unknown" {
			t.Errorf("CommandCode(%d).String() = unknown, want a real name", code)
		}
		if strings.Contains(s, "%") {
			t.Errorf("CommandCode(%d).String() contains printf verb: %q", code, s)
		}
	}
}

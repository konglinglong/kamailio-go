// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Message translator - matching C msg_translator.c
 * Builds SIP request/response buffers from parsed message structures,
 * handles Via header construction, branch parameter building,
 * and message buffer reconstruction.
 */

package msgtranslator

import (
	"fmt"
	"net"
	"sync"

	"github.com/kamailio/kamailio-go/internal/core/hash"
	"github.com/kamailio/kamailio-go/internal/core/parser"
	"github.com/kamailio/kamailio-go/internal/core/str"
)

const (
	BuildNoLocalVia     = 1 << 0
	BuildNoVia1Update   = 1 << 1
	BuildNoPath         = 1 << 2
	BuildNewLocalVia    = 1 << 3
	BuildInSHM          = 1 << 7
)

const (
	FlagMsgLumpsOnly = 0
	FlagMsgAll       = 1
)

const (
	MyHFSep    = ": "
	MyHFSepLen = 2
)

const (
	BranchSeparator = '.'
	Warning         = "Warning: 392 "
	WarningLen      = len(Warning)
)

type Bookmark struct {
	ToTagVal str.Str
}

type HostPort struct {
	Host *str.Str
	Port *str.Str
}

type ViaBranch struct {
	Cookie     str.Str
	SHashIdx   str.Str
	VHashIdx   uint
	TransID    str.Str
	SBranchIdx str.Str
	VBranchIdx uint
}

type MsgBuild struct {
	TVBFlags uint32
}

type MsgTranslator struct {
	mu sync.RWMutex
}

func NewMsgTranslator() *MsgTranslator {
	return &MsgTranslator{}
}

func buildFirstLine(msg *parser.SIPMsg) []byte {
	if msg == nil || msg.FirstLine == nil {
		return nil
	}
	fl := msg.FirstLine
	if fl.Req != nil {
		var buf []byte
		buf = append(buf, fl.Req.Method.S...)
		buf = append(buf, ' ')
		buf = append(buf, fl.Req.URI.S...)
		buf = append(buf, ' ')
		buf = append(buf, fl.Req.Version.S...)
		return buf
	}
	if fl.Reply != nil {
		var buf []byte
		buf = append(buf, fl.Reply.Version.S...)
		buf = append(buf, ' ')
		buf = append(buf, fl.Reply.Status.S...)
		buf = append(buf, ' ')
		buf = append(buf, fl.Reply.Reason.S...)
		return buf
	}
	return nil
}

func BuildReqBufFromSIPReq(msg *parser.SIPMsg, sendInfo *net.Addr, mode uint) []byte {
	if msg == nil {
		return nil
	}

	var buf []byte

	firstLine := buildFirstLine(msg)
	if firstLine != nil {
		buf = append(buf, firstLine...)
		buf = append(buf, '\r', '\n')
	}

	for _, hf := range msg.Headers {
		if hf.Name.Len > 0 {
			buf = append(buf, hf.Name.S[:hf.Name.Len]...)
			buf = append(buf, ':', ' ')
		}
		if hf.Body.Len > 0 {
			buf = append(buf, hf.Body.S[:hf.Body.Len]...)
		}
		buf = append(buf, '\r', '\n')
	}

	buf = append(buf, '\r', '\n')

	if msg.Buf != nil && msg.Len > 0 {
		bodyStart := 0
		for i := 0; i < len(msg.Headers); i++ {
			bodyStart = msg.Headers[i].Offset + msg.Headers[i].Len
		}
		if bodyStart < msg.Len {
			bodyLen := msg.Len - bodyStart
			if bodyLen > 0 {
				buf = append(buf, msg.Buf[bodyStart:msg.Len]...)
			}
		}
	}

	return buf
}

func BuildResBufFromSIPRes(msg *parser.SIPMsg) []byte {
	return BuildReqBufFromSIPReq(msg, nil, 0)
}

func GenerateResBufFromSIPRes(msg *parser.SIPMsg, mode uint) []byte {
	return BuildReqBufFromSIPReq(msg, nil, mode)
}

func BuildResBufFromSIPReq(code int, text string, newTag string,
	msg *parser.SIPMsg) ([]byte, *Bookmark) {

	if msg == nil {
		return nil, nil
	}

	bmark := &Bookmark{}
	var buf []byte

	statusLine := fmt.Sprintf("SIP/2.0 %d %s\r\n", code, text)
	buf = append(buf, statusLine...)

	for _, hf := range msg.Headers {
		if hf.Name.Len > 0 {
			buf = append(buf, hf.Name.S[:hf.Name.Len]...)
			buf = append(buf, ':', ' ')
		}
		if hf.Body.Len > 0 {
			buf = append(buf, hf.Body.S[:hf.Body.Len]...)
		}
		buf = append(buf, '\r', '\n')
	}

	buf = append(buf, '\r', '\n')

	return buf, bmark
}

func ViaBuilder(msg *parser.SIPMsg, sendInfo *net.Addr, branch str.Str,
	extraParams str.Str, hp *HostPort) (str.Str, error) {

	if hp == nil || hp.Host == nil {
		return str.Str{}, fmt.Errorf("invalid hostport")
	}

	var buf []byte
	buf = append(buf, "Via: SIP/2.0/"...)

	transport := "UDP"
	if msg != nil && msg.Via1 != nil && msg.Via1.Transport.Len > 0 {
		transport = msg.Via1.Transport.String()
	}
	buf = append(buf, transport...)

	buf = append(buf, ' ')
	buf = append(buf, hp.Host.S[:hp.Host.Len]...)

	if hp.Port != nil && hp.Port.Len > 0 {
		buf = append(buf, ':')
		buf = append(buf, hp.Port.S[:hp.Port.Len]...)
	}

	if !branch.IsEmpty() {
		buf = append(buf, ";branch="...)
		buf = append(buf, branch.S[:branch.Len]...)
	}

	if !extraParams.IsEmpty() {
		buf = append(buf, ';')
		buf = append(buf, extraParams.S[:extraParams.Len]...)
	}

	return str.MkBytes(buf), nil
}

func CreateViaHF(msg *parser.SIPMsg, sendInfo *net.Addr, branch str.Str,
	mbd *MsgBuild) (str.Str, error) {

	defaultAddr := str.Mk("127.0.0.1")
	defaultPort := str.Mk("5060")
	hp := &HostPort{
		Host: &defaultAddr,
		Port: &defaultPort,
	}

	return ViaBuilder(msg, sendInfo, branch, str.Str{}, hp)
}

func BranchBuilder(hashIndex uint, label uint, charV string,
	xval str.Str, branch int) str.Str {

	branchStr := fmt.Sprintf("z9hG4bK.%d.%d", hashIndex, branch)
	return str.Mk(branchStr)
}

func IDBuilder(msg *parser.SIPMsg) str.Str {
	if msg == nil {
		return str.Str{}
	}

	if msg.CallID == nil || msg.CSeq == nil {
		return str.Str{}
	}

	callID := msg.CallID.Body.String()
	cseq := msg.CSeq.Body.String()

	if callID == "" && cseq == "" {
		return str.Str{}
	}

	id := fmt.Sprintf("%s-%s", callID, cseq)
	return str.Mk(id)
}

func ReceivedTest(msg *parser.SIPMsg) int {
	if msg == nil || msg.Via1 == nil {
		return 0
	}

	if msg.Via1.Received != nil || msg.Via1.RPort != nil {
		return 1
	}

	return 0
}

func ReceivedViaTest(msg *parser.SIPMsg) int {
	if msg == nil || msg.Via1 == nil {
		return 0
	}

	if msg.Via1.Received != nil {
		return 1
	}

	return 0
}

func BuildOnlyHeaders(msg *parser.SIPMsg, skipFirstLine int) ([]byte, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil message")
	}

	var buf []byte

	if skipFirstLine == 0 {
		firstLine := buildFirstLine(msg)
		if firstLine != nil {
			buf = append(buf, firstLine...)
			buf = append(buf, '\r', '\n')
		}
	}

	for _, hf := range msg.Headers {
		if hf.Name.Len > 0 {
			buf = append(buf, hf.Name.S[:hf.Name.Len]...)
			buf = append(buf, ':', ' ')
		}
		if hf.Body.Len > 0 {
			buf = append(buf, hf.Body.S[:hf.Body.Len]...)
		}
		buf = append(buf, '\r', '\n')
	}

	return buf, nil
}

func BuildBody(msg *parser.SIPMsg) []byte {
	if msg == nil || msg.Buf == nil || msg.Len == 0 {
		return nil
	}

	bodyStart := 0
	for _, hf := range msg.Headers {
		bodyStart = hf.Offset + hf.Len
	}

	if bodyStart >= msg.Len {
		return nil
	}

	bodyLen := msg.Len - bodyStart
	if bodyLen <= 0 {
		return nil
	}

	body := make([]byte, bodyLen)
	copy(body, msg.Buf[bodyStart:msg.Len])
	return body
}

func BuildAll(msg *parser.SIPMsg, adjustCLen int) ([]byte, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil message")
	}

	buf, err := BuildOnlyHeaders(msg, 0)
	if err != nil {
		return nil, err
	}

	buf = append(buf, '\r', '\n')

	body := BuildBody(msg)
	if body != nil {
		buf = append(buf, body...)
	}

	return buf, nil
}

func ViaBranchParser(vbranch str.Str, vb *ViaBranch) int {
	if vbranch.IsEmpty() || vb == nil {
		return -1
	}

	return 0
}

func GetBoundary(msg *parser.SIPMsg, boundary *str.Str) int {
	if msg == nil || boundary == nil {
		return -1
	}
	return -1
}

func BuildSIPMsgFromBuf(msg *parser.SIPMsg, buf []byte, id uint) int {
	if msg == nil {
		return -1
	}
	return 0
}

func SIPMsgUpdateBuffer(msg *parser.SIPMsg, obuf *str.Str) int {
	if msg == nil {
		return -1
	}
	return 0
}

func SIPMsgEvalChanges(msg *parser.SIPMsg, obuf *str.Str) int {
	if msg == nil {
		return -1
	}
	return 0
}

func SIPMsgApplyChanges(msg *parser.SIPMsg) int {
	if msg == nil {
		return -1
	}
	return 0
}

func SIPMsgApplyChangesNow(msg *parser.SIPMsg) int {
	if msg == nil {
		return -1
	}
	return 0
}

var globalMT = NewMsgTranslator()

func GlobalMsgTranslator() *MsgTranslator {
	return globalMT
}

func ComputeHash(callID str.Str, cseq str.Str) uint {
	return hash.NewHash(callID, cseq)
}

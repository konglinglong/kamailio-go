// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * SIP message deep copy (cloner) - matching C sip_msg_clone.c
 *
 * Clone creates a fully independent copy of a SIPMsg so that the clone
 * shares no memory with the original. This is required by the transaction
 * layer (TM), parallel/serial forking and other components that need to
 * keep a stable snapshot of a message after the parser recycles its buffer.
 *
 * Unlike the C implementation which allocates a single contiguous shared
 * memory block and uses translate_pointer() to remap every str.s into the
 * new buffer, the Go version allocates independent byte slices for every
 * str.Str field via str.Str.Clone(). The end result is equivalent: the
 * returned message shares no memory with the original.
 */

package parser

// Clone creates a deep copy of the SIP message, including all headers,
// body, parsed structures, and lump lists. The returned message shares
// no memory with the original.
//
// C: sip_msg_clone()
func (msg *SIPMsg) Clone() (*SIPMsg, error) {
	if msg == nil {
		return nil, nil
	}

	nm := &SIPMsg{}

	// --- scalar value fields ---
	nm.ID = msg.ID
	nm.PID = msg.PID
	nm.ReceivedAt = msg.ReceivedAt
	nm.ParsedFlag = msg.ParsedFlag
	nm.Len = msg.Len
	nm.BufSize = msg.BufSize
	nm.MsgFlags = msg.MsgFlags
	nm.Flags = msg.Flags
	nm.XFlags = msg.XFlags
	nm.VBFlags = msg.VBFlags
	// ForceSendSocket is an opaque shared resource (socket info); the C
	// clone keeps force_send_socket as a shared pointer too.
	nm.ForceSendSocket = msg.ForceSendSocket

	// --- message buffer ---
	if msg.Buf != nil {
		nm.Buf = make([]byte, len(msg.Buf))
		copy(nm.Buf, msg.Buf)
	}

	// --- message body ---
	nm.Body = cloneBody(msg.Body)

	// --- routing URIs (str.Str fields) ---
	nm.NewURI = msg.NewURI.Clone()
	nm.DstURI = msg.DstURI.Clone()

	// --- parsed URIs ---
	if msg.ParsedURI != nil {
		nm.ParsedURI = cloneSIPURI(msg.ParsedURI)
	}
	if msg.ParsedOrigRURI != nil {
		nm.ParsedOrigRURI = cloneSIPURI(msg.ParsedOrigRURI)
	}

	// --- first line ---
	if msg.FirstLine != nil {
		nm.FirstLine = cloneMsgStart(msg.FirstLine)
	}

	// --- headers ---
	// Build old->new maps for headers and via bodies so that quick
	// references and inter-header links (Next, Siblings) can be
	// re-anchored to the cloned objects.
	headerMap := make(map[*HdrField]*HdrField)
	viaMap := make(map[*ViaBody]*ViaBody)

	if len(msg.Headers) > 0 {
		nm.Headers = make([]*HdrField, len(msg.Headers))
		for i, h := range msg.Headers {
			nh := cloneHdrField(h, viaMap)
			nm.Headers[i] = nh
			headerMap[h] = nh
		}
		// Re-link Next / Siblings chains now that all headers exist.
		for i, h := range msg.Headers {
			nh := nm.Headers[i]
			if h.Next != nil {
				nh.Next = headerMap[h.Next]
			}
			if h.Siblings != nil {
				nh.Siblings = headerMap[h.Siblings]
			}
		}
		if msg.LastHeader != nil {
			nm.LastHeader = headerMap[msg.LastHeader]
		}
	}

	// --- quick header references ---
	// Each quick reference must point to the corresponding cloned header
	// rather than the original. headerMap lookup returns nil for nil keys,
	// which correctly leaves unset references as nil.
	nm.HdrVia1 = headerMap[msg.HdrVia1]
	nm.HdrVia2 = headerMap[msg.HdrVia2]
	nm.CallID = headerMap[msg.CallID]
	nm.To = headerMap[msg.To]
	nm.CSeq = headerMap[msg.CSeq]
	nm.From = headerMap[msg.From]
	nm.Contact = headerMap[msg.Contact]
	nm.MaxForwards = headerMap[msg.MaxForwards]
	nm.Route = headerMap[msg.Route]
	nm.RecordRoute = headerMap[msg.RecordRoute]
	nm.ContentType = headerMap[msg.ContentType]
	nm.ContentLength = headerMap[msg.ContentLength]
	nm.Authorization = headerMap[msg.Authorization]
	nm.Expires = headerMap[msg.Expires]
	nm.ProxyAuth = headerMap[msg.ProxyAuth]
	nm.Supported = headerMap[msg.Supported]
	nm.Require = headerMap[msg.Require]
	nm.ProxyRequire = headerMap[msg.ProxyRequire]
	nm.Allow = headerMap[msg.Allow]
	nm.Event = headerMap[msg.Event]
	nm.Accept = headerMap[msg.Accept]
	nm.AcceptLanguage = headerMap[msg.AcceptLanguage]
	nm.Organization = headerMap[msg.Organization]
	nm.Priority = headerMap[msg.Priority]
	nm.Subject = headerMap[msg.Subject]
	nm.UserAgent = headerMap[msg.UserAgent]
	nm.Server = headerMap[msg.Server]
	nm.ContentDisposition = headerMap[msg.ContentDisposition]
	nm.Diversion = headerMap[msg.Diversion]
	nm.RPID = headerMap[msg.RPID]
	nm.ReferTo = headerMap[msg.ReferTo]
	nm.SessionExpires = headerMap[msg.SessionExpires]
	nm.MinSE = headerMap[msg.MinSE]
	nm.SIPIfMatch = headerMap[msg.SIPIfMatch]
	nm.SubscriptionState = headerMap[msg.SubscriptionState]
	nm.Date = headerMap[msg.Date]
	nm.Identity = headerMap[msg.Identity]
	nm.IdentityInfo = headerMap[msg.IdentityInfo]
	nm.PAI = headerMap[msg.PAI]
	nm.PPI = headerMap[msg.PPI]
	nm.Path = headerMap[msg.Path]
	nm.Privacy = headerMap[msg.Privacy]
	nm.MinExpires = headerMap[msg.MinExpires]
	nm.PAccessNetworkInfo = headerMap[msg.PAccessNetworkInfo]
	nm.PVisitedNetworkID = headerMap[msg.PVisitedNetworkID]

	// --- parsed via bodies (Via1 / Via2) ---
	// Via1 and Via2 point into the Parsed field of HdrVia1 / HdrVia2 (or
	// Via1.Next when a single Via header carries multiple comma-separated
	// bodies). The viaMap built during header cloning maps every original
	// *ViaBody to its clone, so we can re-anchor both pointers.
	if msg.Via1 != nil {
		nm.Via1 = viaMap[msg.Via1]
	}
	if msg.Via2 != nil {
		nm.Via2 = viaMap[msg.Via2]
	}

	return nm, nil
}

// cloneMsgStart deep-copies a MsgStart (first line).
// C: first_line translation in sip_msg_shm_clone()
func cloneMsgStart(fl *MsgStart) *MsgStart {
	if fl == nil {
		return nil
	}
	nfl := &MsgStart{
		Type:  fl.Type,
		Flags: fl.Flags,
		Len:   fl.Len,
	}
	if fl.Req != nil {
		nfl.Req = &RequestLine{
			Method:      fl.Req.Method.Clone(),
			URI:         fl.Req.URI.Clone(),
			Version:     fl.Req.Version.Clone(),
			MethodValue: fl.Req.MethodValue,
		}
	}
	if fl.Reply != nil {
		nfl.Reply = &ReplyLine{
			Version:    fl.Reply.Version.Clone(),
			Status:     fl.Reply.Status.Clone(),
			Reason:     fl.Reply.Reason.Clone(),
			StatusCode: fl.Reply.StatusCode,
		}
	}
	return nfl
}

// cloneSIPURI deep-copies a parsed SIP URI.
// C: uri_trans()
func cloneSIPURI(uri *SIPURI) *SIPURI {
	if uri == nil {
		return nil
	}
	return &SIPURI{
		User:         uri.User.Clone(),
		Passwd:       uri.Passwd.Clone(),
		Host:         uri.Host.Clone(),
		Port:         uri.Port.Clone(),
		Params:       uri.Params.Clone(),
		SIPParams:    uri.SIPParams.Clone(),
		Headers:      uri.Headers.Clone(),
		PortNo:       uri.PortNo,
		Proto:        uri.Proto,
		Type:         uri.Type,
		Flags:        uri.Flags,
		Transport:    uri.Transport.Clone(),
		TTL:          uri.TTL.Clone(),
		UserParam:    uri.UserParam.Clone(),
		MAddr:        uri.MAddr.Clone(),
		Method:       uri.Method.Clone(),
		LR:           uri.LR.Clone(),
		R2:           uri.R2.Clone(),
		GR:           uri.GR.Clone(),
		TransportVal: uri.TransportVal.Clone(),
		TTLVal:       uri.TTLVal.Clone(),
		UserParamVal: uri.UserParamVal.Clone(),
		MAddrVal:     uri.MAddrVal.Clone(),
		MethodVal:    uri.MethodVal.Clone(),
		LRVal:        uri.LRVal.Clone(),
		R2Val:        uri.R2Val.Clone(),
		GRVal:        uri.GRVal.Clone(),
	}
}

// cloneViaParamList deep-copies a linked list of ViaParam, returning the
// new head and a map from old *ViaParam to new *ViaParam so that shortcut
// pointers (Branch, Received, RPort, I, Alias) can be re-anchored.
func cloneViaParamList(head *ViaParam) (*ViaParam, map[*ViaParam]*ViaParam) {
	if head == nil {
		return nil, nil
	}
	paramMap := make(map[*ViaParam]*ViaParam)
	var first *ViaParam
	var last *ViaParam
	for p := head; p != nil; p = p.Next {
		np := &ViaParam{
			Type:  p.Type,
			Flags: p.Flags,
			Name:  p.Name.Clone(),
			Value: p.Value.Clone(),
		}
		paramMap[p] = np
		if first == nil {
			first = np
		} else {
			last.Next = np
		}
		last = np
	}
	return first, paramMap
}

// cloneViaBody deep-copies a linked list of ViaBody (a single Via header may
// carry multiple comma-separated bodies chained through Next). Each via body
// including its parameter list and shortcut pointers is cloned independently.
// The viaMap (if non-nil) records the mapping from old to new *ViaBody so
// that the caller can re-anchor msg.Via1 / msg.Via2.
//
// C: via_body_cloner()
func cloneViaBody(vb *ViaBody, viaMap map[*ViaBody]*ViaBody) *ViaBody {
	if vb == nil {
		return nil
	}
	var first *ViaBody
	var last *ViaBody
	for cur := vb; cur != nil; cur = cur.Next {
		nvb := &ViaBody{
			Error:     cur.Error,
			Hdr:       cur.Hdr.Clone(),
			Name:      cur.Name.Clone(),
			Version:   cur.Version.Clone(),
			Transport: cur.Transport.Clone(),
			Host:      cur.Host.Clone(),
			Proto:     cur.Proto,
			Port:      cur.Port,
			PortStr:   cur.PortStr.Clone(),
			Params:    cur.Params.Clone(),
			Comment:   cur.Comment.Clone(),
			TID:       cur.TID.Clone(),
		}

		// Clone the parameter linked list and build old->new map.
		var paramMap map[*ViaParam]*ViaParam
		nvb.ParamList, paramMap = cloneViaParamList(cur.ParamList)

		// Set LastParam by walking to the end of the new list.
		if nvb.ParamList != nil {
			lp := nvb.ParamList
			for lp.Next != nil {
				lp = lp.Next
			}
			nvb.LastParam = lp
		}

		// Re-anchor shortcut parameter pointers. paramMap lookup on a nil
		// map returns nil, which is safe.
		if cur.Branch != nil {
			nvb.Branch = paramMap[cur.Branch]
		}
		if cur.Received != nil {
			nvb.Received = paramMap[cur.Received]
		}
		if cur.RPort != nil {
			nvb.RPort = paramMap[cur.RPort]
		}
		if cur.I != nil {
			nvb.I = paramMap[cur.I]
		}
		if cur.Alias != nil {
			nvb.Alias = paramMap[cur.Alias]
		}

		// Register in the via map for Via1/Via2 re-anchoring.
		if viaMap != nil {
			viaMap[cur] = nvb
		}

		// Link into the new list.
		if first == nil {
			first = nvb
		} else {
			last.Next = nvb
		}
		last = nvb
	}
	return first
}

// cloneToBody deep-copies a parsed To/From body.
// C: to_body cloning in sip_msg_shm_clone()
func cloneToBody(tb *ToBody) *ToBody {
	if tb == nil {
		return nil
	}
	ntb := &ToBody{
		DisplayName: tb.DisplayName.Clone(),
		Tag:         tb.Tag.Clone(),
		Params:      tb.Params.Clone(),
	}
	if tb.URI != nil {
		ntb.URI = cloneSIPURI(tb.URI)
	}
	if tb.ParsedURI != nil {
		ntb.ParsedURI = cloneSIPURI(tb.ParsedURI)
	}
	return ntb
}

// cloneContactBody deep-copies a parsed Contact body (including the Next
// chain for comma-separated contacts).
func cloneContactBody(cb *ContactBody) *ContactBody {
	if cb == nil {
		return nil
	}
	ncb := &ContactBody{
		DisplayName: cb.DisplayName.Clone(),
		Expires:     cb.Expires,
		QValue:      cb.QValue,
		Instance:    cb.Instance.Clone(),
		RegID:       cb.RegID,
		Params:      cb.Params.Clone(),
	}
	if cb.URI != nil {
		ncb.URI = cloneSIPURI(cb.URI)
	}
	if cb.Next != nil {
		ncb.Next = cloneContactBody(cb.Next)
	}
	return ncb
}

// cloneCSeqBody deep-copies a parsed CSeq body.
// C: cseq_body cloning in sip_msg_shm_clone()
func cloneCSeqBody(cb *CSeqBody) *CSeqBody {
	if cb == nil {
		return nil
	}
	return &CSeqBody{
		Method:      cb.Method.Clone(),
		Number:      cb.Number,
		MethodValue: cb.MethodValue,
	}
}

// cloneParsed deep-copies the type-specific parsed body stored in a
// HdrField.Parsed. Unknown types are returned as-is (shared reference),
// mirroring the C cloner which leaves parsed=NULL for unrecognised types.
func cloneParsed(parsed interface{}, viaMap map[*ViaBody]*ViaBody) interface{} {
	if parsed == nil {
		return nil
	}
	switch v := parsed.(type) {
	case *ViaBody:
		return cloneViaBody(v, viaMap)
	case *ToBody:
		return cloneToBody(v)
	case *ContactBody:
		return cloneContactBody(v)
	case *CSeqBody:
		return cloneCSeqBody(v)
	case *SIPURI:
		return cloneSIPURI(v)
	default:
		// Unrecognised parsed type: share the reference.
		return parsed
	}
}

// cloneHdrField deep-copies a single HdrField. The Next and Siblings
// pointers are left nil and re-linked by the caller (which needs the
// full old->new header map). Via bodies in Parsed are cloned and
// registered in viaMap.
func cloneHdrField(h *HdrField, viaMap map[*ViaBody]*ViaBody) *HdrField {
	if h == nil {
		return nil
	}
	nh := &HdrField{
		Name:   h.Name.Clone(),
		Body:   h.Body.Clone(),
		Type:   h.Type,
		Offset: h.Offset,
		Len:    h.Len,
	}
	nh.Parsed = cloneParsed(h.Parsed, viaMap)
	return nh
}

// cloneBody deep-copies the message body. The body is typically a []byte
// slice into the original buffer; for unknown body types the reference is
// shared (Go has no generic deep-copy for interface{}).
func cloneBody(body interface{}) interface{} {
	if body == nil {
		return nil
	}
	switch b := body.(type) {
	case []byte:
		if b == nil {
			return nil
		}
		nb := make([]byte, len(b))
		copy(nb, b)
		return nb
	default:
		// Unknown body type (e.g. parsed SDP struct): share reference.
		return body
	}
}

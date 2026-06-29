// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Lump processing - matching C data_lump_rpl.c::process_lumps()
 * and data_lump.c::process_lumps().
 *
 * Applies a list of lump operations (insert/delete/replace) against
 * the original message buffer to produce a rebuilt buffer. Lumps
 * reference offsets into the original buffer; this function walks
 * the original buffer while emitting the new buffer, applying each
 * lump in offset order and recursing into Before/After sub-lumps.
 */

package msgtranslator

import (
	"bytes"
	"errors"
	"sort"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// ErrInvalidLumpOffset is returned when a lump references an offset
// outside the original buffer.
var ErrInvalidLumpOffset = errors.New("msgtranslator: lump offset out of range")

// indexedLump is an ordered lump entry used during sorting. It carries
// a pointer to the source lump and a stable sequence number so that
// lumps registered earlier sort before later ones when offsets match.
type indexedLump struct {
	lump *parser.Lump
	seq  uint64
}

// ProcessLumps applies a LumpList against buf and returns the rebuilt
// buffer. The original buffer is never mutated.
//
// Semantics (mirroring C process_lumps):
//   - LumpDel: bytes [Offset, Offset+Len) are removed.
//   - LumpAdd with LumpFlagIsBefore: NewValue is inserted before Offset.
//   - LumpAdd with LumpFlagIsAfter: NewValue is inserted at Offset (treated
//     as an insert at Offset).
//   - LumpAdd with LumpFlagIsDel (replace): bytes [Offset, Offset+Len)
//     are removed and NewValue is inserted at Offset.
//
// Before/After sub-lumps of each lump are emitted around the anchor
// point: Before sub-lumps first, then the lump's own effect, then
// After sub-lumps.
//
// It is safe for concurrent use: the lump list is read-only during
// processing.
func ProcessLumps(buf []byte, lumps *parser.LumpList) ([]byte, error) {
	if lumps == nil || lumps.Head == nil {
		// No lumps: return a copy so callers can mutate freely.
		out := make([]byte, len(buf))
		copy(out, buf)
		return out, nil
	}

	// Collect and sort the lump chain by offset, preserving registration
	// order for ties via a sequence counter.
	ordered, err := collectOrdered(lumps)
	if err != nil {
		return nil, err
	}

	var out bytes.Buffer
	srcPos := 0
	for _, il := range ordered {
		l := il.lump
		// Validate offset.
		if l.Offset < 0 || l.Offset > len(buf) {
			return nil, ErrInvalidLumpOffset
		}
		// Copy any bytes before this lump's offset that haven't been
		// emitted yet.
		if l.Offset > srcPos {
			out.Write(buf[srcPos:l.Offset])
			srcPos = l.Offset
		}

		// Emit Before sub-lumps first. Sub-lumps are inserts anchored to
		// the parent lump; they emit their NewValue directly without
		// consuming source bytes.
		if l.Before != nil {
			for b := l.Before; b != nil; b = b.Next {
				if b.NewValue != nil {
					out.Write(b.NewValue)
				}
			}
		}

		// Apply the lump's own operation.
		switch {
		case l.Op == parser.LumpDel:
			// Pure delete: advance srcPos past the deleted range.
			end := l.Offset + l.Len
			if end > len(buf) {
				end = len(buf)
			}
			srcPos = end
		case l.Op == parser.LumpAdd && (l.Flags&parser.LumpFlagIsDel) != 0:
			// Replace: advance past deleted range, then insert new value.
			end := l.Offset + l.Len
			if end > len(buf) {
				end = len(buf)
			}
			if l.NewValue != nil {
				out.Write(l.NewValue)
			}
			srcPos = end
		case l.Op == parser.LumpAdd:
			// Pure insert: emit value, do not advance srcPos.
			if l.NewValue != nil {
				out.Write(l.NewValue)
			}
		}

		// Emit After sub-lumps.
		if l.After != nil {
			for a := l.After; a != nil; a = a.Next {
				if a.NewValue != nil {
					out.Write(a.NewValue)
				}
			}
		}
	}

	// Copy any trailing bytes after the last lump.
	if srcPos < len(buf) {
		out.Write(buf[srcPos:])
	}

	return out.Bytes(), nil
}

// ProcessMsgLumps applies all lump lists of a MsgLumps structure against
// buf in a single pass. All lumps reference absolute offsets into the
// original buffer, so the lists are merged and sorted together rather
// than applied sequentially (sequential application would invalidate
// offsets after the first list modifies the buffer).
//
// The merge order preserves the C semantics: head lumps, add/rm
// (header) lumps, body lumps, and reply lumps all reference offsets
// into the original message buffer and are applied together.
func ProcessMsgLumps(buf []byte, lumps *parser.MsgLumps) ([]byte, error) {
	if lumps == nil {
		out := make([]byte, len(buf))
		copy(out, buf)
		return out, nil
	}

	// Merge all lump lists into a single ordered list. We flatten the
	// four lists into one chain, preserving per-list registration order,
	// then let collectOrdered sort by offset.
	merged := &parser.LumpList{}
	appendChain(merged, lumps.HeadLumps.Head)
	appendChain(merged, lumps.AddRM.Head)
	appendChain(merged, lumps.BodyLumps.Head)
	appendChain(merged, lumps.ReplyLumps.Head)

	return ProcessLumps(buf, merged)
}

// appendChain appends a deep copy of a lump chain to dst. We clone each
// node so that mutating Next pointers during the merge does not corrupt
// the source lists (which may be reused across rebuilds).
func appendChain(dst *parser.LumpList, head *parser.Lump) {
	for l := head; l != nil; l = l.Next {
		clone := &parser.Lump{
			Op:       l.Op,
			Flags:    l.Flags,
			Offset:   l.Offset,
			Len:      l.Len,
			NewValue: l.NewValue, // NewValue is treated as read-only here
			Before:   l.Before,    // shared read-only sub-chain
			After:    l.After,     // shared read-only sub-chain
		}
		dst.Append(clone)
	}
}

// ApplyLumps is the high-level entry point that takes a parsed SIPMsg
// and a MsgLumps, and returns the rebuilt message buffer. It uses
// msg.Buf as the source. Returns an error if msg has no buffer.
func ApplyLumps(msg *parser.SIPMsg, lumps *parser.MsgLumps) ([]byte, error) {
	if msg == nil {
		return nil, errors.New("msgtranslator: nil message")
	}
	if msg.Buf == nil || msg.Len == 0 {
		return nil, errors.New("msgtranslator: message has no buffer")
	}
	// Use msg.Len rather than len(msg.Buf) so callers can pass buffers
	// that are larger than the live message.
	src := msg.Buf[:msg.Len]
	return ProcessMsgLumps(src, lumps)
}

// collectOrdered flattens the LumpList chain into a sorted slice.
// Lumps are ordered by Offset ascending; ties preserve registration
// order via a monotonic sequence counter.
func collectOrdered(lumps *parser.LumpList) ([]indexedLump, error) {
	var ordered []indexedLump
	var seq uint64
	for l := lumps.Head; l != nil; l = l.Next {
		ordered = append(ordered, indexedLump{lump: l, seq: seq})
		seq++
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].lump.Offset != ordered[j].lump.Offset {
			return ordered[i].lump.Offset < ordered[j].lump.Offset
		}
		// Before-anchored inserts should precede the lump at the same
		// offset; pure deletes should precede inserts at the same offset
		// so the deleted bytes do not get emitted before the insert.
		fi, fj := ordered[i].lump.Flags, ordered[j].lump.Flags
		if (fi&parser.LumpFlagIsBefore) != (fj&parser.LumpFlagIsBefore) {
			return (fi & parser.LumpFlagIsBefore) != 0
		}
		// Otherwise preserve registration order.
		return ordered[i].seq < ordered[j].seq
	})
	return ordered, nil
}

// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * math - arithmetic operations for routing scripts.
 *
 * Provides a small expression evaluator plus the usual rounding and
 * power helpers. Mirrors the kamailio math module.
 */

package math

import (
	"errors"
	"math"
	"strconv"
)

// MathModule exposes arithmetic helpers to routing scripts.
type MathModule struct{}

// New returns a new MathModule.
func New() *MathModule { return &MathModule{} }

// Eval evaluates an arithmetic expression consisting of decimal numbers,
// parentheses and the operators + - * /. Returns the result or an error
// for malformed input or division by zero.
func (m *MathModule) Eval(expr string) (float64, error) {
	if m == nil {
		return 0, errors.New("math: nil module")
	}
	p := &parser{s: expr}
	p.skipSpaces()
	v, err := p.parseExpr()
	if err != nil {
		return 0, err
	}
	p.skipSpaces()
	if p.pos != len(p.s) {
		return 0, errors.New("math: trailing characters in expression")
	}
	return v, nil
}

// Round rounds val to the given number of decimal places.
func (m *MathModule) Round(val float64, precision int) float64 {
	if m == nil {
		return 0
	}
	pow := math.Pow(10, float64(precision))
	return math.Round(val*pow) / pow
}

// Floor returns the largest integer not greater than val.
func (m *MathModule) Floor(val float64) float64 {
	if m == nil {
		return 0
	}
	return math.Floor(val)
}

// Ceil returns the smallest integer not less than val.
func (m *MathModule) Ceil(val float64) float64 {
	if m == nil {
		return 0
	}
	return math.Ceil(val)
}

// Sqrt returns the square root of val.
func (m *MathModule) Sqrt(val float64) float64 {
	if m == nil {
		return 0
	}
	return math.Sqrt(val)
}

// Pow returns base raised to the power exp.
func (m *MathModule) Pow(base, exp float64) float64 {
	if m == nil {
		return 0
	}
	return math.Pow(base, exp)
}

// ---------------------------------------------------------------------------
// recursive-descent expression parser
// ---------------------------------------------------------------------------

type parser struct {
	s   string
	pos int
}

func (p *parser) skipSpaces() {
	for p.pos < len(p.s) && (p.s[p.pos] == ' ' || p.s[p.pos] == '\t') {
		p.pos++
	}
}

func (p *parser) parseExpr() (float64, error) {
	v, err := p.parseTerm()
	if err != nil {
		return 0, err
	}
	for {
		p.skipSpaces()
		if p.pos >= len(p.s) {
			break
		}
		switch p.s[p.pos] {
		case '+':
			p.pos++
			r, err := p.parseTerm()
			if err != nil {
				return 0, err
			}
			v += r
		case '-':
			p.pos++
			r, err := p.parseTerm()
			if err != nil {
				return 0, err
			}
			v -= r
		default:
			return v, nil
		}
	}
	return v, nil
}

func (p *parser) parseTerm() (float64, error) {
	v, err := p.parseFactor()
	if err != nil {
		return 0, err
	}
	for {
		p.skipSpaces()
		if p.pos >= len(p.s) {
			break
		}
		switch p.s[p.pos] {
		case '*':
			p.pos++
			r, err := p.parseFactor()
			if err != nil {
				return 0, err
			}
			v *= r
		case '/':
			p.pos++
			r, err := p.parseFactor()
			if err != nil {
				return 0, err
			}
			if r == 0 {
				return 0, errors.New("math: division by zero")
			}
			v /= r
		default:
			return v, nil
		}
	}
	return v, nil
}

func (p *parser) parseFactor() (float64, error) {
	p.skipSpaces()
	if p.pos >= len(p.s) {
		return 0, errors.New("math: unexpected end of expression")
	}
	switch p.s[p.pos] {
	case '(':
		p.pos++
		v, err := p.parseExpr()
		if err != nil {
			return 0, err
		}
		p.skipSpaces()
		if p.pos >= len(p.s) || p.s[p.pos] != ')' {
			return 0, errors.New("math: missing closing parenthesis")
		}
		p.pos++
		return v, nil
	case '-':
		p.pos++
		v, err := p.parseFactor()
		return -v, err
	case '+':
		p.pos++
		return p.parseFactor()
	}
	start := p.pos
	for p.pos < len(p.s) {
		c := p.s[p.pos]
		if (c >= '0' && c <= '9') || c == '.' {
			p.pos++
		} else {
			break
		}
	}
	if start == p.pos {
		return 0, errors.New("math: unexpected character: " + string(p.s[p.pos]))
	}
	return strconv.ParseFloat(p.s[start:p.pos], 64)
}

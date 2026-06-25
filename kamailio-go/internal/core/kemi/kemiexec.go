// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * KEMI execution engine - matching Kamailio core kemiexec.c.
 *
 * Provides the ExecContext that carries per-request mutable state
 * (variables, AVPs, result) and the Execute / ExecuteChain methods
 * that drive KEMI function calls. Also provides parameter validation
 * and type conversion helpers that mirror C's sr_kemi_exec_func()
 * dispatch logic.
 *
 * Go counterpart of C's sr_kemi_exec_func() and the parameter-type
 * dispatch in kemiexec.c.
 */

package kemi

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/kamailio/kamailio-go/internal/core/avp"
	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// ExecContext carries mutable per-request state for KEMI execution.
// It is the Go counterpart of C's run_act_ctx_t plus the script
// variables and AVP store used during request processing.
type ExecContext struct {
	// Msg is the SIP message being processed.
	Msg *parser.SIPMsg
	// Vars holds script variables ($var(name)) for this execution.
	Vars map[string]string
	// AVPs holds per-request AVP values.
	AVPs *avp.Store
	// Result is the return code of the last executed function.
	Result int
	// Error records the first error encountered during execution.
	Error error
}

// NewExecContext creates a fresh execution context for the given
// message. msg may be nil (e.g. in unit tests).
func NewExecContext(msg *parser.SIPMsg) *ExecContext {
	return &ExecContext{
		Msg:  msg,
		Vars: make(map[string]string),
		AVPs: avp.NewStore(),
	}
}

// FuncCall describes a single KEMI function invocation within a chain.
type FuncCall struct {
	// Module is the module name (may be empty for bare-name lookup).
	Module string
	// Name is the function name.
	Name string
	// Params are the parameters to pass to the function.
	Params []interface{}
}

// Execute executes a single KEMI function call against the context. It
// finds the function, validates its parameters, invokes it and stores
// the result in ctx.Result. If the function is not found or parameter
// validation fails, ctx.Error is set and KemiFalse is returned. A
// KemiDrop (0) return from the function stops further processing.
func (e *Engine) Execute(ctx *ExecContext, funcName string, params ...interface{}) int {
	if ctx == nil {
		return KemiFalse
	}
	exp := e.FindFunc(funcName)
	if exp == nil {
		ctx.Error = fmt.Errorf("kemi function %q not found", funcName)
		ctx.Result = KemiFalse
		return KemiFalse
	}
	if err := e.ValidateParams(exp, params); err != nil {
		ctx.Error = err
		ctx.Result = KemiFalse
		return KemiFalse
	}
	ret := exp.Func(ctx.Msg, params...)
	ctx.Result = ret
	return ret
}

// ExecuteChain executes a sequence of KEMI function calls in order. The
// chain stops early if any call returns KemiDrop (0) or KemiFalse
// (-1), matching Kamailio's script execution semantics where a
// negative return aborts the route. The last return code is stored in
// ctx.Result and returned.
func (e *Engine) ExecuteChain(ctx *ExecContext, calls []FuncCall) int {
	if ctx == nil {
		return KemiFalse
	}
	lastRet := KemiTrue
	for _, c := range calls {
		var ret int
		var err error
		if c.Module != "" {
			ret, err = e.CallByModule(c.Module, c.Name, ctx.Msg, c.Params...)
		} else {
			ret, err = e.Call(c.Name, ctx.Msg, c.Params...)
		}
		if err != nil {
			ctx.Error = err
			ctx.Result = KemiFalse
			return KemiFalse
		}
		lastRet = ret
		ctx.Result = ret
		// Drop stops processing entirely.
		if ret == KemiDrop {
			return KemiDrop
		}
		// False aborts the chain (matching Kamailio script semantics).
		if ret == KemiFalse {
			return KemiFalse
		}
	}
	if lastRet == 0 && len(calls) == 0 {
		lastRet = KemiTrue
	}
	return lastRet
}

// ValidateParams checks that the given parameters satisfy the export's
// MinParams/MaxParams constraints and that each parameter is
// compatible with its declared ParamType. It mirrors the parameter
// validation performed by C's sr_kemi_exec_func() before dispatch.
func (e *Engine) ValidateParams(exp *KemiExport, params []interface{}) error {
	if exp == nil {
		return fmt.Errorf("nil function export")
	}
	n := len(params)
	if n < exp.MinParams {
		return fmt.Errorf("function %q requires at least %d params, got %d",
			exp.Name, exp.MinParams, n)
	}
	if exp.MaxParams != VarParams && n > exp.MaxParams {
		return fmt.Errorf("function %q accepts at most %d params, got %d",
			exp.Name, exp.MaxParams, n)
	}
	// Validate each parameter against its declared type when available.
	for i, p := range params {
		if i >= len(exp.ParamTypes) {
			break
		}
		pt := exp.ParamTypes[i]
		if pt == ParamAny {
			continue
		}
		if _, err := ConvertParam(p, pt); err != nil {
			return fmt.Errorf("function %q param %d: %w", exp.Name, i+1, err)
		}
	}
	return nil
}

// ConvertParam converts val to the target parameter type. It supports
// the common conversions needed by KEMI functions: strings to
// ints/bools, ints to strings/bools, etc. If val is already
// compatible it is returned as-is. This mirrors the type coercion
// performed by C's embedded language bindings before calling
// sr_kemi_exec_func().
func ConvertParam(val interface{}, targetType ParamType) (interface{}, error) {
	if targetType == ParamAny {
		return val, nil
	}
	switch targetType {
	case ParamString:
		return convertToString(val)
	case ParamInt:
		return convertToInt(val)
	case ParamBool:
		return convertToBool(val)
	case ParamPVar:
		// PVar names are strings.
		s, err := convertToString(val)
		if err != nil {
			return nil, err
		}
		return s, nil
	default:
		return nil, fmt.Errorf("unsupported target type %d", int(targetType))
	}
}

// convertToString converts val to a string.
func convertToString(val interface{}) (string, error) {
	if val == nil {
		return "", fmt.Errorf("nil cannot be converted to string")
	}
	switch v := val.(type) {
	case string:
		return v, nil
	case []byte:
		return string(v), nil
	case int:
		return strconv.Itoa(v), nil
	case int32:
		return strconv.FormatInt(int64(v), 10), nil
	case int64:
		return strconv.FormatInt(v, 10), nil
	case uint:
		return strconv.FormatUint(uint64(v), 10), nil
	case uint32:
		return strconv.FormatUint(uint64(v), 10), nil
	case uint64:
		return strconv.FormatUint(v, 10), nil
	case bool:
		if v {
			return "true", nil
		}
		return "false", nil
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), nil
	case fmt.Stringer:
		return v.String(), nil
	default:
		// Fall back to reflection for other numeric types.
		rv := reflect.ValueOf(val)
		switch rv.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16,
			reflect.Int32, reflect.Int64:
			return strconv.FormatInt(rv.Int(), 10), nil
		case reflect.Uint, reflect.Uint8, reflect.Uint16,
			reflect.Uint32, reflect.Uint64:
			return strconv.FormatUint(rv.Uint(), 10), nil
		case reflect.Float32, reflect.Float64:
			return strconv.FormatFloat(rv.Float(), 'f', -1, 64), nil
		case reflect.Bool:
			if rv.Bool() {
				return "true", nil
			}
			return "false", nil
		}
		return "", fmt.Errorf("cannot convert %T to string", val)
	}
}

// convertToInt converts val to an int.
func convertToInt(val interface{}) (int, error) {
	if val == nil {
		return 0, fmt.Errorf("nil cannot be converted to int")
	}
	switch v := val.(type) {
	case int:
		return v, nil
	case int32:
		return int(v), nil
	case int64:
		return int(v), nil
	case uint:
		return int(v), nil
	case uint32:
		return int(v), nil
	case uint64:
		return int(v), nil
	case bool:
		if v {
			return 1, nil
		}
		return 0, nil
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return 0, fmt.Errorf("cannot parse %q as int: %w", v, err)
		}
		return n, nil
	case []byte:
		n, err := strconv.Atoi(strings.TrimSpace(string(v)))
		if err != nil {
			return 0, fmt.Errorf("cannot parse %q as int: %w", string(v), err)
		}
		return n, nil
	case float64:
		return int(v), nil
	default:
		rv := reflect.ValueOf(val)
		switch rv.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16,
			reflect.Int32, reflect.Int64:
			return int(rv.Int()), nil
		case reflect.Uint, reflect.Uint8, reflect.Uint16,
			reflect.Uint32, reflect.Uint64:
			return int(rv.Uint()), nil
		case reflect.Float32, reflect.Float64:
			return int(rv.Float()), nil
		case reflect.Bool:
			if rv.Bool() {
				return 1, nil
			}
			return 0, nil
		}
		return 0, fmt.Errorf("cannot convert %T to int", val)
	}
}

// convertToBool converts val to a bool.
func convertToBool(val interface{}) (bool, error) {
	if val == nil {
		return false, fmt.Errorf("nil cannot be converted to bool")
	}
	switch v := val.(type) {
	case bool:
		return v, nil
	case int:
		return v != 0, nil
	case int32:
		return v != 0, nil
	case int64:
		return v != 0, nil
	case uint:
		return v != 0, nil
	case uint32:
		return v != 0, nil
	case uint64:
		return v != 0, nil
	case string:
		b, err := parseBool(v)
		if err != nil {
			return false, fmt.Errorf("cannot parse %q as bool: %w", v, err)
		}
		return b, nil
	case []byte:
		b, err := parseBool(string(v))
		if err != nil {
			return false, fmt.Errorf("cannot parse %q as bool: %w", string(v), err)
		}
		return b, nil
	case float64:
		return v != 0, nil
	default:
		rv := reflect.ValueOf(val)
		switch rv.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16,
			reflect.Int32, reflect.Int64:
			return rv.Int() != 0, nil
		case reflect.Uint, reflect.Uint8, reflect.Uint16,
			reflect.Uint32, reflect.Uint64:
			return rv.Uint() != 0, nil
		case reflect.Float32, reflect.Float64:
			return rv.Float() != 0, nil
		case reflect.Bool:
			return rv.Bool(), nil
		}
		return false, fmt.Errorf("cannot convert %T to bool", val)
	}
}

// parseBool accepts the same string values as Kamailio's boolean
// parameters: true/false, yes/no, on/off, 1/0 (case-insensitive).
func parseBool(s string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on":
		return true, nil
	case "0", "false", "no", "off":
		return false, nil
	}
	return strconv.ParseBool(s)
}

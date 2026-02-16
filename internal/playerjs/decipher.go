package playerjs

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/dop251/goja"
)

// Decipherer handles signature deciphering and n-parameter transformation.
type Decipherer struct {
	jsBody []byte

	runtimeOnce sync.Once
	runtime     *runtimeDecipherer
	runtimeErr  error
}

func NewDecipherer(jsBody string) *Decipherer {
	return &Decipherer{
		jsBody: []byte(jsBody),
	}
}

// DecipherSignature deciphers the 's' parameter.
func (d *Decipherer) DecipherSignature(s string) (string, error) {
	ops, err := d.parseDecipherOps()
	if err == nil {
		bs := []byte(s)
		for _, op := range ops {
			bs = op(bs)
		}
		return string(bs), nil
	}

	decoded, runtimeErr := d.decipherSignatureWithRuntime(s)
	if runtimeErr == nil {
		return decoded, nil
	}
	return "", err
}

// DecipherN deciphers the 'n' parameter.
func (d *Decipherer) DecipherN(n string) (string, error) {
	fn, err := d.getNFunction()
	if err == nil {
		decoded, evalErr := evalJavascript(fn, n)
		if evalErr == nil {
			return decoded, nil
		}
	}

	decoded, runtimeErr := d.decipherNWithRuntime(n)
	if runtimeErr == nil {
		return decoded, nil
	}
	if err != nil {
		return "", err
	}
	return "", runtimeErr
}

type DecipherOperation func([]byte) []byte

const (
	jsVarStr   = "[a-zA-Z_\\$][a-zA-Z_0-9]*"
	reverseStr = ":function\\(a\\)\\{" +
		"(?:return )?a\\.reverse\\(\\)" +
		"\\}"
	spliceStr = ":function\\(a,b\\)\\{" +
		"a\\.splice\\(0,b\\)" +
		"\\}"
	swapStr = ":function\\(a,b\\)\\{" +
		"var c=a\\[0\\];a\\[0\\]=a\\[b(?:%a\\.length)?\\];a\\[b(?:%a\\.length)?\\]=c(?:;return a)?" +
		"\\}"
)

var (
	nFunctionNameRegexps = []*regexp.Regexp{
		// Original kkdai pattern kept for compatibility with existing fixtures.
		regexp.MustCompile(`\.get\("n"\)\)&&\(b=([a-zA-Z0-9$]{0,3})\[(\d+)\](.+)\|\|([a-zA-Z0-9]{0,3})`),
		// Legacy pattern: b=XY[0](b)||ZZ
		regexp.MustCompile(`\.get\("n"\)\)\s*&&\s*\(b=([a-zA-Z0-9$]{1,})\[(\d+)\]\([a-zA-Z0-9$]{1,}\).+\|\|([a-zA-Z0-9$]{1,})`),
		// Newer pattern: b=XY(b)
		regexp.MustCompile(`\.get\("n"\)\)\s*&&\s*\(b=([a-zA-Z0-9$]{1,})\([a-zA-Z0-9$]{1,}\)`),
		// Some variants use optional chaining / looser spacing.
		regexp.MustCompile(`\.get\("n"\).*?&&.*?([a-zA-Z0-9$]{1,})\([a-zA-Z0-9$]{1,}\)`),
	}
	actionsObjRegexp = regexp.MustCompile(fmt.Sprintf(
		"(?:var|let|const)\\s+(%s)=\\{((?:(?:%s%s|%s%s|%s%s),?\\n?)+)\\}\\s*;?",
		jsVarStr, jsVarStr, swapStr, jsVarStr, spliceStr, jsVarStr, reverseStr))
	reverseRegexp      = regexp.MustCompile(fmt.Sprintf("(?m)(?:^|,)(%s)%s", jsVarStr, reverseStr))
	spliceRegexp       = regexp.MustCompile(fmt.Sprintf("(?m)(?:^|,)(%s)%s", jsVarStr, spliceStr))
	swapRegexp         = regexp.MustCompile(fmt.Sprintf("(?m)(?:^|,)(%s)%s", jsVarStr, swapStr))
	actionsFuncRegexps = []*regexp.Regexp{
		// function XX(a){...}
		regexp.MustCompile(fmt.Sprintf(
			"function(?:\\s+%s)?\\(a\\)\\{"+
				"a=a\\.split\\([^\\)]*\\);\\s*"+
				"((?:(?:a=)?%s(?:\\.%s|\\[[^\\]]+\\])\\(a,\\d+\\);?\\s*)+)"+
				"return a\\.join\\([^\\)]*\\)"+
				"\\}", jsVarStr, jsVarStr, jsVarStr)),
		// XX=function(a){...}
		regexp.MustCompile(fmt.Sprintf(
			"%s\\s*=\\s*function\\(a\\)\\{"+
				"a=a\\.split\\([^\\)]*\\);\\s*"+
				"((?:(?:a=)?%s(?:\\.%s|\\[[^\\]]+\\])\\(a,\\d+\\);?\\s*)+)"+
				"return a\\.join\\([^\\)]*\\)"+
				"\\}", jsVarStr, jsVarStr, jsVarStr)),
	}
)

func (d *Decipherer) parseDecipherOps() ([]DecipherOperation, error) {
	objResult := actionsObjRegexp.FindSubmatch(d.jsBody)
	funcBody := d.findActionsFuncBody()
	if len(objResult) < 3 || len(funcBody) == 0 {
		return nil, fmt.Errorf("error parsing signature tokens (#obj=%d, #func=%d)", len(objResult), len(funcBody))
	}

	obj := objResult[1]
	objBody := objResult[2]

	var reverseKey, spliceKey, swapKey string
	if result := reverseRegexp.FindSubmatch(objBody); len(result) > 1 {
		reverseKey = string(result[1])
	}
	if result := spliceRegexp.FindSubmatch(objBody); len(result) > 1 {
		spliceKey = string(result[1])
	}
	if result := swapRegexp.FindSubmatch(objBody); len(result) > 1 {
		swapKey = string(result[1])
	}

	regex, err := regexp.Compile(fmt.Sprintf(
		"(?:a=)?%s(?:\\.(%s|%s|%s)|\\[(?:\"(%s|%s|%s)\"|'(%s|%s|%s)')\\])\\(a,(\\d+)\\)",
		regexp.QuoteMeta(string(obj)),
		regexp.QuoteMeta(reverseKey),
		regexp.QuoteMeta(spliceKey),
		regexp.QuoteMeta(swapKey),
		regexp.QuoteMeta(reverseKey),
		regexp.QuoteMeta(spliceKey),
		regexp.QuoteMeta(swapKey),
		regexp.QuoteMeta(reverseKey),
		regexp.QuoteMeta(spliceKey),
		regexp.QuoteMeta(swapKey),
	))
	if err != nil {
		return nil, err
	}

	var ops []DecipherOperation
	for _, s := range regex.FindAllSubmatch(funcBody, -1) {
		if len(s) < 5 {
			continue
		}
		key := firstNonEmptySubmatch(s[1], s[2], s[3])
		arg, _ := strconv.Atoi(string(s[4]))
		switch key {
		case reverseKey:
			ops = append(ops, reverseFunc)
		case swapKey:
			ops = append(ops, newSwapFunc(arg))
		case spliceKey:
			ops = append(ops, newSpliceFunc(arg))
		}
	}
	if len(ops) == 0 {
		return nil, fmt.Errorf("error parsing signature operations (empty op list)")
	}
	return ops, nil
}

func (d *Decipherer) findActionsFuncBody() []byte {
	for _, re := range actionsFuncRegexps {
		if m := re.FindSubmatch(d.jsBody); len(m) > 1 {
			return m[1]
		}
	}
	return nil
}

func (d *Decipherer) getNFunction() (string, error) {
	for _, re := range nFunctionNameRegexps {
		nameResult := re.FindSubmatch(d.jsBody)
		if len(nameResult) == 0 {
			continue
		}

		switch len(nameResult) {
		case 5:
			// Original pattern with explicit fallback symbol in group 4.
			if idx, err := strconv.Atoi(string(nameResult[2])); err == nil && idx == 0 {
				return d.extractFunction(string(nameResult[4]))
			}
			return d.extractFunction(string(nameResult[1]))
		case 4:
			// Legacy pattern with indexed function and fallback symbol.
			if idx, err := strconv.Atoi(string(nameResult[2])); err == nil && idx == 0 {
				return d.extractFunction(string(nameResult[3]))
			}
			return d.extractFunction(string(nameResult[1]))
		default:
			// Direct call pattern.
			return d.extractFunction(string(nameResult[1]))
		}
	}
	return "", errors.New("unable to extract n-function name")
}

func (d *Decipherer) extractFunction(name string) (string, error) {
	name = strings.TrimSpace(name)
	defPatterns := [][]byte{
		[]byte(name + "=function("),
		[]byte(name + " = function("),
		[]byte("function " + name + "("),
	}
	start := -1
	for _, def := range defPatterns {
		start = bytes.Index(d.jsBody, def)
		if start >= 0 {
			break
		}
	}
	if start < 0 {
		return "", fmt.Errorf("unable to extract n-function body")
	}

	pos := start + bytes.IndexByte(d.jsBody[start:], '{') + 1
	var strChar byte
	for brackets := 1; brackets > 0; pos++ {
		if pos >= len(d.jsBody) {
			return "", fmt.Errorf("unterminated n-function body")
		}
		b := d.jsBody[pos]
		switch b {
		case '{':
			if strChar == 0 {
				brackets++
			}
		case '}':
			if strChar == 0 {
				brackets--
			}
		case '`', '"', '\'':
			if pos > 1 && d.jsBody[pos-1] == '\\' && d.jsBody[pos-2] != '\\' {
				continue
			}
			if strChar == 0 {
				strChar = b
			} else if strChar == b {
				strChar = 0
			}
		}
	}
	return string(d.jsBody[start:pos]), nil
}

func firstNonEmptySubmatch(groups ...[]byte) string {
	for _, g := range groups {
		if len(g) > 0 {
			return string(g)
		}
	}
	return ""
}

func evalJavascript(jsFunction, arg string) (string, error) {
	const fnName = "ytv1NsigFunction"
	vm := goja.New()
	if _, err := vm.RunString(fnName + "=" + jsFunction); err != nil {
		return "", err
	}
	var output func(string) string
	if err := vm.ExportTo(vm.Get(fnName), &output); err != nil {
		return "", err
	}
	return output(arg), nil
}

type runtimeDecipherer struct {
	mu       sync.Mutex
	vm       *goja.Runtime
	sigFunc  goja.Callable
	nURLFunc goja.Callable
}

var (
	signatureRuntimeNameRegexp = regexp.MustCompile(`const\s+[A-Za-z0-9_$]+=([A-Za-z0-9_$]+)\(16,decodeURIComponent\([^\)]*\.s\)\)`)
	nURLRuntimeNameRegexp      = regexp.MustCompile(`([A-Za-z0-9_$]+)=function\(b\)\{try\{const\s+[A-Za-z0-9_$]+=\(new\s+g\.Mj\(b,!0\)\)\.get\("n"\)`)
	nPathExtractRegexp         = regexp.MustCompile(`/n/([^/?]+)`)
)

func (d *Decipherer) decipherSignatureWithRuntime(s string) (string, error) {
	rt, err := d.loadRuntimeDecipherer()
	if err != nil {
		return "", err
	}
	if rt.sigFunc == nil {
		return "", errors.New("runtime signature decipher function unavailable")
	}

	rt.mu.Lock()
	defer rt.mu.Unlock()

	out, err := rt.sigFunc(goja.Undefined(), rt.vm.ToValue(16), rt.vm.ToValue(s))
	if err != nil {
		return "", err
	}
	return out.String(), nil
}

func (d *Decipherer) decipherNWithRuntime(n string) (string, error) {
	rt, err := d.loadRuntimeDecipherer()
	if err != nil {
		return "", err
	}
	if rt.nURLFunc == nil {
		return "", errors.New("runtime n decipher function unavailable")
	}

	escaped := url.PathEscape(n)
	inputURL := "https://www.youtube.com/videoplayback/n/" + escaped + "/x?n=" + url.QueryEscape(n)

	rt.mu.Lock()
	out, err := rt.nURLFunc(goja.Undefined(), rt.vm.ToValue(inputURL))
	rt.mu.Unlock()
	if err != nil {
		return "", err
	}

	m := nPathExtractRegexp.FindStringSubmatch(out.String())
	if len(m) < 2 {
		return "", errors.New("runtime n decipher output missing /n/ segment")
	}
	decoded, decodeErr := url.PathUnescape(m[1])
	if decodeErr != nil {
		return "", decodeErr
	}
	return decoded, nil
}

func (d *Decipherer) loadRuntimeDecipherer() (*runtimeDecipherer, error) {
	d.runtimeOnce.Do(func() {
		d.runtime, d.runtimeErr = d.buildRuntimeDecipherer()
	})
	return d.runtime, d.runtimeErr
}

func (d *Decipherer) buildRuntimeDecipherer() (*runtimeDecipherer, error) {
	jsBody := string(d.jsBody)
	sigName := ""
	nURLName := ""

	if m := signatureRuntimeNameRegexp.FindStringSubmatch(jsBody); len(m) > 1 {
		sigName = m[1]
	}
	if m := nURLRuntimeNameRegexp.FindStringSubmatch(jsBody); len(m) > 1 {
		nURLName = m[1]
	}
	if sigName == "" && nURLName == "" {
		return nil, errors.New("runtime decipher export points not found")
	}

	inject := ""
	if sigName != "" {
		inject += "g.__ytv1_sig=" + sigName + ";"
	}
	if nURLName != "" {
		inject += "g.__ytv1_nurl=" + nURLName + ";"
	}

	const marker = "})(_yt_player);"
	markerPos := strings.LastIndex(jsBody, marker)
	if markerPos < 0 {
		return nil, errors.New("unable to inject runtime decipher exports")
	}
	jsBody = jsBody[:markerPos] + inject + jsBody[markerPos:]

	vm := goja.New()
	if _, err := vm.RunString(runtimePreludeJS); err != nil {
		return nil, err
	}
	if _, err := vm.RunString(jsBody); err != nil {
		return nil, err
	}

	root := vm.Get("_yt_player")
	if root == nil || goja.IsUndefined(root) || goja.IsNull(root) {
		return nil, errors.New("runtime decipher missing _yt_player root")
	}
	rootObj := root.ToObject(vm)

	rt := &runtimeDecipherer{vm: vm}
	if sigVal := rootObj.Get("__ytv1_sig"); sigVal != nil && !goja.IsUndefined(sigVal) && !goja.IsNull(sigVal) {
		if fn, ok := goja.AssertFunction(sigVal); ok {
			rt.sigFunc = fn
		}
	}
	if nURLVal := rootObj.Get("__ytv1_nurl"); nURLVal != nil && !goja.IsUndefined(nURLVal) && !goja.IsNull(nURLVal) {
		if fn, ok := goja.AssertFunction(nURLVal); ok {
			rt.nURLFunc = fn
		}
	}
	if rt.sigFunc == nil && rt.nURLFunc == nil {
		return nil, errors.New("runtime decipher exports are not callable")
	}
	return rt, nil
}

const runtimePreludeJS = `
var globalThis = this;
if (typeof window === 'undefined') { var window = this; }
if (typeof document === 'undefined') { var document = {}; }
if (typeof navigator === 'undefined') { var navigator = {}; }
if (typeof self === 'undefined') { var self = this; }
if (typeof location === 'undefined') {
	var location = {
		href: 'https://www.youtube.com/watch?v=ytv1',
		protocol: 'https:',
		host: 'www.youtube.com',
		hostname: 'www.youtube.com',
		pathname: '/watch',
		search: '?v=ytv1',
		hash: '',
		origin: 'https://www.youtube.com'
	};
}
if (!window.location) { window.location = location; }
if (!window.navigator) { window.navigator = navigator; }
if (!window.document) { window.document = document; }
if (!window.top) { window.top = window; }
if (!window.parent) { window.parent = window; }
if (!window.performance) {
	window.performance = { now: function(){ return 0; }, mark: function(){}, measure: function(){}, clearMarks: function(){} };
}
if (!window.localStorage) {
	window.localStorage = { getItem: function(){ return null; }, setItem: function(){}, removeItem: function(){} };
}
if (!window.sessionStorage) {
	window.sessionStorage = { getItem: function(){ return null; }, setItem: function(){}, removeItem: function(){} };
}
if (!window.setTimeout) { window.setTimeout = function(fn){ return 0; }; }
if (!window.clearTimeout) { window.clearTimeout = function(){}; }
if (!window.setInterval) { window.setInterval = function(fn){ return 0; }; }
if (!window.clearInterval) { window.clearInterval = function(){}; }
if (!window.addEventListener) { window.addEventListener = function(){}; }
if (!window.removeEventListener) { window.removeEventListener = function(){}; }
if (!window.matchMedia) {
	window.matchMedia = function(){ return { matches: false, addListener: function(){}, removeListener: function(){} }; };
}
if (!window.crypto) {
	window.crypto = {
		getRandomValues: function(arr){ for (var i = 0; i < arr.length; i++) { arr[i] = 0; } return arr; }
	};
}
if (typeof XMLHttpRequest === 'undefined') {
	var XMLHttpRequest = function(){};
	XMLHttpRequest.prototype = {
		open: function(){},
		send: function(){},
		setRequestHeader: function(){},
		addEventListener: function(){},
		removeEventListener: function(){},
		getResponseHeader: function(){ return ''; },
		abort: function(){},
		readyState: 4,
		status: 200,
		responseText: '',
		response: null
	};
}
if (!window.XMLHttpRequest) { window.XMLHttpRequest = XMLHttpRequest; }
if (typeof Intl === 'undefined') { var Intl = {}; }
if (!Intl.DateTimeFormat) {
	Intl.DateTimeFormat = function(){ return { resolvedOptions: function(){ return { timeZone: 'UTC' }; } }; };
}
if (!Intl.NumberFormat) {
	Intl.NumberFormat = function(){ return { format: function(v){ return String(v); } }; };
	Intl.NumberFormat.supportedLocalesOf = function(){ return []; };
}
if (!Intl.PluralRules) {
	Intl.PluralRules = function(){ return { select: function(){ return 'other'; } }; };
	Intl.PluralRules.supportedLocalesOf = function(){ return []; };
}
if (!Intl.RelativeTimeFormat) {
	Intl.RelativeTimeFormat = function(){ return { format: function(v, u){ return String(v) + ' ' + String(u); } }; };
	Intl.RelativeTimeFormat.supportedLocalesOf = function(){ return []; };
}
if (!document.createElement) {
	document.createElement = function(){
		return {
			style: {},
			getContext: function(){ return null; },
			canPlayType: function(){ return ''; },
			setAttribute: function(){},
			removeAttribute: function(){},
			appendChild: function(){},
			addEventListener: function(){},
			removeEventListener: function(){}
		};
	};
}
if (!document.querySelectorAll) { document.querySelectorAll = function(){ return []; }; }
if (!document.getElementsByTagName) { document.getElementsByTagName = function(){ return []; }; }
if (!document.addEventListener) { document.addEventListener = function(){}; }
if (!document.removeEventListener) { document.removeEventListener = function(){}; }
if (!document.location) { document.location = window.location; }
if (!document.documentElement) { document.documentElement = { style: {} }; }
`

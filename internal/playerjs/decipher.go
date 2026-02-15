package playerjs

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strconv"

	"github.com/dop251/goja"
)

// Decipherer handles signature deciphering and n-parameter transformation.
type Decipherer struct {
	jsBody []byte
}

func NewDecipherer(jsBody string) *Decipherer {
	return &Decipherer{
		jsBody: []byte(jsBody),
	}
}

// DecipherSignature deciphers the 's' parameter.
func (d *Decipherer) DecipherSignature(s string) (string, error) {
	ops, err := d.parseDecipherOps()
	if err != nil {
		return "", err
	}
	bs := []byte(s)
	for _, op := range ops {
		bs = op(bs)
	}
	return string(bs), nil
}

// DecipherN deciphers the 'n' parameter.
func (d *Decipherer) DecipherN(n string) (string, error) {
	fn, err := d.getNFunction()
	if err != nil {
		return "", err
	}
	return evalJavascript(fn, n)
}

type DecipherOperation func([]byte) []byte

const (
	jsVarStr = "[a-zA-Z_\\$][a-zA-Z_0-9]*"
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
	actionsObjRegexp    = regexp.MustCompile(fmt.Sprintf(
		"(?:var|let|const)\\s+(%s)=\\{((?:(?:%s%s|%s%s|%s%s),?\\n?)+)\\}\\s*;?",
		jsVarStr, jsVarStr, swapStr, jsVarStr, spliceStr, jsVarStr, reverseStr))
	actionsFuncRegexp = regexp.MustCompile(fmt.Sprintf(
		"function(?: %s)?\\(a\\)\\{"+
			"a=a\\.split\\([^\\)]*\\);\\s*"+
			"((?:(?:a=)?%s\\.%s\\(a,\\d+\\);)+)"+
			"return a\\.join\\([^\\)]*\\)"+
			"\\}", jsVarStr, jsVarStr, jsVarStr))
	reverseRegexp = regexp.MustCompile(fmt.Sprintf("(?m)(?:^|,)(%s)%s", jsVarStr, reverseStr))
	spliceRegexp  = regexp.MustCompile(fmt.Sprintf("(?m)(?:^|,)(%s)%s", jsVarStr, spliceStr))
	swapRegexp    = regexp.MustCompile(fmt.Sprintf("(?m)(?:^|,)(%s)%s", jsVarStr, swapStr))
)

func (d *Decipherer) parseDecipherOps() ([]DecipherOperation, error) {
	objResult := actionsObjRegexp.FindSubmatch(d.jsBody)
	funcResult := actionsFuncRegexp.FindSubmatch(d.jsBody)
	if len(objResult) < 3 || len(funcResult) < 2 {
		return nil, fmt.Errorf("error parsing signature tokens (#obj=%d, #func=%d)", len(objResult), len(funcResult))
	}

	obj := objResult[1]
	objBody := objResult[2]
	funcBody := funcResult[1]

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
		"(?:a=)?%s\\.(%s|%s|%s)\\(a,(\\d+)\\)",
		regexp.QuoteMeta(string(obj)),
		regexp.QuoteMeta(reverseKey),
		regexp.QuoteMeta(spliceKey),
		regexp.QuoteMeta(swapKey),
	))
	if err != nil {
		return nil, err
	}

	var ops []DecipherOperation
	for _, s := range regex.FindAllSubmatch(funcBody, -1) {
		switch string(s[1]) {
		case reverseKey:
			ops = append(ops, reverseFunc)
		case swapKey:
			arg, _ := strconv.Atoi(string(s[2]))
			ops = append(ops, newSwapFunc(arg))
		case spliceKey:
			arg, _ := strconv.Atoi(string(s[2]))
			ops = append(ops, newSpliceFunc(arg))
		}
	}
	return ops, nil
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
	def := []byte(name + "=function(")
	start := bytes.Index(d.jsBody, def)
	if start < 1 {
		def = []byte("function " + name + "(")
		start = bytes.Index(d.jsBody, def)
		if start < 1 {
			return "", fmt.Errorf("unable to extract n-function body")
		}
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

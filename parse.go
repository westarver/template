package template

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/westarver/helper"
	//trace "github.com/westarver/trace" //<rmv/>

	"github.com/bitfield/script"
	"golang.design/x/clipboard"
)

const defExt = ".exec"

//────────────┤ ParseFiles ├────────────
//Output will be appended to existing files.
func ParseFiles(inputs []string, outputs []string, w io.Writer) int {
	var n, k int
	var in, out string
	var err error

	//the following is compensation for docopts inability to
	//recognize the -- as a separator
	tmpls, outs := helper.SplitOnDashDash(inputs, outputs)

	if len(tmpls) == 0 {
		o, _ := os.Stdin.Stat()
		if (o.Mode() & os.ModeCharDevice) == os.ModeCharDevice { //Terminal
			tmpls = append(tmpls, "stdin-term")
		} else {
			tmpls = append(tmpls, "stdin-pipe")
		}
	}
	if len(outs) == 0 {
		outs = append(outs, "-") //"template"+defExt)
	}

	//match up input files with output files accounting for mismatched lengths
	matched := matchio(tmpls, outs)

	for i, t := range matched {
		if t.in == "stdin-term" || t.in == "stdin-pipe" {
			in = t.in
		} else {
			in, _ = helper.GetPath(t.in) // ensure it is an absolute in
		}

		out = t.out
		if out != "-" {
			out, _ = helper.GetPath(t.out)
		}
		if len(in) > 0 && len(out) > 0 {
			k, err = parseFile(in, out, w)
		}
		if err != nil {
			fmt.Fprintf(w, "%+v", err)
		}
		if k != 0 {
			n = i + 1
		}
	}
	return n
} // ParseFiles

type pair struct {
	in  string
	out string
}

//─────────────────────┤ matchio ├─────────────────────
//match up input files with output files accounting for
//mismatched lengths of slices. Extra outs will be ignored,
//and by default extra ins will match to the last out,
//appending output from multiple templates into a single
//out file. If no output file names are given, matchio will
//simply replace the extension of each template name to "EXEC"
//to generate the output file names.
//This behaviour can be changed by offering a 'wildcard'
//as an out file name. An out file given as "/s suffix"
//will concatenate "suffix" to in file to create the out
//file name ('source.tpl' becomes 'source.tplsuffix').
//Using "/S suffix" will append the suffix to the basename,
//the extension remains ("source.tpl >> sourcesuffix.tpl").
//A wildcard of "/p prefix" will prepend the given text
//('source.tpl >> 'prefixsource.tpl).  Using "/e ext"
//will replace the extension of the template with the given
//extension ('source.tpl >> source.ext'). To replace the
//extension with a blank use a / as the arg for /e.
//("/e /" results in 'src.ex.tpl >> src.ex')
//These can be combined as in "/d/p/s/S/e dir pre suf ext".
//As expected "/p/e pre ext" will add the prefix and
//replace the extension. "/S/e suffix ext" will produce
//'sourcesuffix.ext'.
//One more wildcard: "/d directory" will prepend the given dir
//to any output file name however derived. The dir will be
//created in the cwd if it does not exist.
//The above examples showed the directives separated with a /
//for visual reasons only. They can be but it's not required.
//"/d/p/e d p e" is equivalent to "/dpe d p e".  The directives
//will work in any order, but the args must be in the same order
//as the directives they refer to.
func matchio(ins []string, outs []string) []pair {
	var matched []pair

	for _, f := range ins {
		matched = append(matched, pair{f, "x"}) //out is dummy string for now
	}

	ilen := len(ins)
	olen := len(outs)

	if olen == 0 { // easy one first
		for i := 0; i < len(matched); i++ {
			matched[i].out = matched[i].in + defExt
		}
		return matched
	}

	mismatch := ilen - olen
	if mismatch < 0 {
		mismatch = 0 //discard extra outs
	}
	if mismatch == 0 { // all ins have a matching out. match them up.  later we check for wildcards.
		for i := 0; i < len(matched); i++ {
			matched[i].out = outs[i]
		}
	} else { // here we know there is at least one out but fewer outs than ins
		for i := 0; i < olen; i++ { //copy as many outs as were given to the matched slice
			matched[i].out = outs[i]
		}
		for i := olen; i < ilen; i++ { //fill in remainder of matched slice with last out
			matched[i].out = outs[olen-1]
		}
	}

	// now we check for and parse wildcards
	for i, m := range matched {
		if strings.HasPrefix(m.out, "/") {
			// maybe not a file name but a slash directive,  but
			// we do need to check for a rooted absolute path
			// if it is we assume that was intended
			// use 2 slashes to denote that intent
			// ex. //home/me/my/stuff/template.tpl
			if len(m.out) > 1 && m.out[1] == '/' {
				matched[i].out = m.out[1:]
				continue
			}

			ss := strings.Split(m.out, " ")
			//ss holds slash directives in ss[0] and the individual tokens in the remaining indices
			tmp := m.in // file name is going to be the same as input with additions

			// get rid of slashes
			var direct string
			for _, c := range ss[0] {
				if c == '/' {
					continue
				}
				direct += string(c)
			}
			// get slice of args
			var args []string
			args = append(args, ss[1:]...)
			// if there are fewer args tha directives the
			// following loop would panic due to index out of range
			if len(args) < len(direct) {
				matched[i].out = m.in + defExt
				continue
			}
			//start a new loop
			for argnum, op := range direct {
				switch op {
				case 'd':
					d := args[argnum]
					if !strings.HasSuffix(d, "/") {
						d += "/"
					}
					if strings.HasPrefix(tmp, "../") {
						tmp = tmp[2:]
					}
					if strings.HasPrefix(tmp, "./") {
						tmp = tmp[1:]
					}
					tmp = d + tmp
				case 'p':
					pre := args[argnum]
					p, _ := filepath.Split(tmp)
					b := filepath.Base(tmp)
					tmp = p + pre + b
				case 'S':
					s := args[argnum]
					p, _ := filepath.Split(tmp)
					e := filepath.Ext(tmp)
					b := strings.TrimSuffix(filepath.Base(tmp), e)
					tmp = p + b + s + e
				case 'n':
					n := args[argnum]
					p, _ := filepath.Split(tmp)
					e := filepath.Ext(tmp)
					tmp = p + n + e
				case 'e':
					e := args[argnum]
					if !strings.HasPrefix(e, ".") {
						e = "." + e
					}
					// user used "/" as ext meaning remove the ext
					if e == "./" {
						e = ""
					}
					p, _ := filepath.Split(tmp)
					x := filepath.Ext(tmp)
					b := strings.TrimSuffix(filepath.Base(tmp), x)
					tmp = p + b + e
				case 's':
					tmp += args[argnum]
				default: // unrecognized char
					continue
				}
			}
			matched[i].out = tmp
			if len(m.out) == 0 {
				matched[i].out = m.in + defExt
			}
		}
	}
	return matched
} // matchio

//───────────────────┤ parseFile ├───────────────────

func parseFile(path string, out string, w io.Writer) (int, error) {
	var data bytes.Buffer
	var err error
	var o *os.File
	var n int

	if out == "-" {
		o = os.Stdout
	} else {
		for trys := 0; trys < 3; trys++ {
			o, err = os.OpenFile(out, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if err != nil {
				p, f := filepath.Split(out)
				err = os.MkdirAll(p, 0777)
				if err != nil {
					fmt.Fprintf(w, "cannot make directory %+v", err)
					return 0, err
				}
				out = p + f
				continue
			}
			break
		}
		defer o.Close()
	}

	if path == "stdin-term" {
		fmt.Println(os.Args[0])
		fmt.Print(">> ")
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			fmt.Print(">> ")
			fmt.Fprintln(&data, scanner.Text())
		}
		if err := scanner.Err(); err != nil {
			err = errors.New("reading standard input: " + err.Error())
			return 0, err
		}
		n = parse(data.Bytes(), o, w)
		return n, nil
	}

	if path == "stdin-pipe" {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			fmt.Fprintln(&data, scanner.Text())
		}
		if err := scanner.Err(); err != nil {
			err = errors.New("reading standard input pipe: " + err.Error())
			return 0, err
		}
		n = parse(data.Bytes(), o, w)
		return n, nil
	}

	dat, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	fmt.Fprintf(w, "Executing %s\n", path)
	n = parse(dat, o, w)
	return n, nil
}

//────────────────┤ parse ├────────────────

func parse(b []byte, out *os.File, w io.Writer) int {
	var funcMap = map[string]scanfunc{
		"clip": clipFunc,
		"env":  envFunc,
		"exec": execFunc,
		"file": fileFunc,
		"var":  varFunc,
		"let":  letFunc,
	}

	var tmpl = template{
		text:     string(b),
		expanded: strings.Builder{},
		vars:     make(map[string]string),
		funcs:    funcMap,
		pos:      0,
		i:        0,
		width:    0,
		line:     0,
	}

	scanf := scanText(&tmpl)
	for {
		if scanf == nil {
			break
		}
		scanf = scanf(&tmpl)
	}

	var n int
	if out != nil {
		n, _ = fmt.Fprintln(out, tmpl.expanded.String())
		fmt.Fprintf(w, "wrote %d bytes to %s\n", n, out.Name())
	}
	return n
}

//─────────────┤ splitOnDashDash ├─────────────

func splitOnDashDash(inputs, outputs []string) ([]string, []string) {
	for i, s := range outputs {
		if s == "--" {
			inputs = append(inputs, outputs[i+1:]...)
			return inputs, outputs[:i]
		}
	}
	return inputs, outputs
}

// --------------------------------------
//start of scanning functions and related

const (
	LPar        = `\050`
	RPar        = `\051`
	LBrk        = `\133`
	RBrk        = `\135`
	LBrc        = `\123`
	RBrc        = `\125`
	Aster       = `\052`
	WS0         = `[\t ]*`
	WS1         = `[\t ]+`
	Any         = `[.]+`
	TemplatePat = LBrc + LBrc + Any + RBrc + RBrc
	IdentList   = IdentPat + WS0 + `(,` + WS1 + IdentPat + `)*`
	IdentPat    = `_?[A-Za-z_]+[0-9]*`
)

const (
	eof        = -1
	newline    = 10
	tab        = 9
	leftDelim  = LBrc + LBrc
	rightDelim = RBrc + RBrc
)

type scanfunc func(*template) scanfunc

type template struct {
	text     string
	expanded strings.Builder
	vars     map[string]string
	funcs    map[string]scanfunc
	pos      int
	i        int
	width    int
	line     int
}

func (t *template) next() rune {
	if int(t.pos) >= len(t.text) {
		t.width = 0
		return eof
	}
	r, w := utf8.DecodeRuneInString(t.text[t.pos:])
	t.width = w
	t.pos += t.width
	if r == '\n' {
		t.line++
	}
	return r
}

func (t *template) peek() rune {
	if int(t.pos) >= len(t.text) {
		t.width = 0
		return eof
	}
	r, _ := utf8.DecodeRuneInString(t.text[t.pos:])
	return r
}

func (t *template) backup() {
	t.pos -= t.width
	// Correct newline count.
	if t.width == 1 && t.text[t.pos] == '\n' {
		t.line--
	}
}

func (t *template) skipws() {
	for {
		r := t.next()
		if !unicode.IsSpace(r) {
			t.backup()
			break
		}
	}
}

func (t *template) puts(word string) {
	t.expanded.WriteString(word)
}

func (t *template) storeVar(v, val string) {
	t.vars[v] = val
}

func (t *template) getVar(v string) string {
	return t.vars[v]
}

func (t *template) Pos() int {
	return t.pos
}

func (t *template) insert(s string) {
	//var trace = trace.New(os.Stdout)    //<rmv/>
	//defer trace.Trace("leaving insert") //<rmv/>
	//trace.Trace("entering insert")      //<rmv/>

	lhs := t.text[:t.Pos()]
	rhs := t.text[t.Pos():]
	t.text = lhs + s + rhs
	//trace.Trace("insert s ", t.text) //<rmv/>
}

func (t *template) toNextDelim(delim string) (string, int) {
	//var trace = trace.New(os.Stdout)      //<rmv/>
	//trace.Trace("entering t.toNextDelim") //<rmv/>

	var ret []rune
	if ok := strings.Contains(t.text[t.Pos():], delim); ok {
		i := strings.Index(t.text[t.Pos():], delim)
		//trace.Trace("t.toNextDelim found ", delim, " at ", t.pos+i) //<rmv/>
		pos := t.Pos()
		for j := pos; j < pos+i; j++ {
			ret = append(ret, t.next())
			//trace.Trace("ret = ", string(ret)) //<rmv/>
		}
		//trace.Trace("t.pos is ", t.Pos(), " and i is ", i) //<rmv/>
		//trace.Trace("t.toNextDelim returns *", string(ret), "*") //<rmv/>
		return string(ret), t.Pos()
	}
	return t.text[t.Pos():], eof
}

//─────────────┤ scanText ├─────────────

func scanText(t *template) scanfunc {
	//trace := trace.New(os.Stdout)    //<rmv/>
	//trace.Trace("entering scanText") //<rmv/>
	lhs, newpos := getToDelim(t, leftDelim)
	//trace.Trace("t.pos is now ", t.Pos(), "*", newpos) //<rmv/>
	//trace.Trace("lhs is *", lhs, "*") //<rmv/>
	t.puts(lhs)
	if newpos > 0 {
		return scanForFunc
	}
	return nil
}

//─────────────┤ scanForFunc ├─────────────

func scanForFunc(t *template) scanfunc {
	//trace := trace.New(os.Stdout)	//<rmv/>
	//trace.Trace("entering scanForFunc")	//<rmv/>

	word := getNextWord(t)
	//race.Trace("scanForFunc found ", word) //<rmv/>

	if word == "/*" {
		return scanComment
	}
	if fn := t.funcs[word]; fn != nil {
		return fn
	}
	//trace.Trace("returning from scanForFunc with nil") //<rmv/>
	return nil
}

//─────────────┤ scanToEnd ├─────────────

func scanToEnd(t *template) scanfunc {
	//var trace = trace.New(os.Stdout)  //<rmv/>
	//trace.Trace("entering scanToEnd") //<rmv/>
	_, _ = getToDelim(t, rightDelim)
	return scanText
}

//─────────────┤ scanComment ├─────────────

func scanComment(t *template) scanfunc {
	//var trace = trace.New(os.Stdout)    //<rmv/>
	//trace.Trace("entering scanComment") //<rmv/>
	return scanToEnd(t)
}

//─────────────┤ getNextWord ├─────────────

func getNextWord(t *template) string {
	//trace := trace.New(os.Stdout) //<rmv/>
	//trace.Trace("entering getNextWord") //<rmv/>
	t.skipws()
	var ret []rune
	var r rune
	for {
		r = t.next()
		//trace.Trace("rune is ", r) //<rmv/>
		if r == eof {
			break
		}
		if unicode.IsSpace(r) {
			t.backup()
			break
		}
		if r == '/' {
			if t.peek() == '*' {
				r2 := t.next()
				ret = append(ret, r)
				ret = append(ret, r2)
				break
			}
		}
		if r == '}' {
			if t.peek() == '}' {
				t.backup()
				break
			}
		}
		ret = append(ret, r)
	}
	//trace.Trace("leaving getNextWord with ", string(ret))//<rmv/>
	return string(ret)
}

//─────────────┤ getToDelim ├─────────────

func getToDelim(t *template, delim string) (string, int) {
	//var trace = trace.New(os.Stdout) //<rmv/>
	s, n := t.toNextDelim(delim)
	//trace.Trace("s = ", s) //<rmv/>
	for i := 0; i < len(delim); i++ {
		_ = t.next()
		//trace.Trace("r = ", string(r)) //<rmv/>
		n++
	}
	return s, n
}

//─────────────┤ clipFunc ├─────────────

func clipFunc(t *template) scanfunc {
	err := clipboard.Init()
	if err == nil {
		clip := "//---------------\n" + string(clipboard.Read(clipboard.FmtText))
		if len(clip) == 0 {
			t.puts("clipboard") //could not get clipboard. just move on
		} else {
			t.puts(clip)
		}
	}
	return scanToEnd
}

//─────────────┤ envFunc ├─────────────

func envFunc(t *template) scanfunc {
	word := getNextWord(t)
	if len(word) == 0 {
		//trace.Trace("no arg to env function") //<rmv/>
		return scanToEnd
	}
	if !strings.HasPrefix(word, "$") {
		//trace.Trace("no $ sign before ", word) //<rmv/>
		t.puts(word)
		return scanToEnd
	}
	en := os.Getenv(word[1:])
	if len(en) == 0 {
		t.puts(word) //could not translate. just move on
		//trace.Trace("env var ", word, " is not set") //<rmv/>
	} else {
		t.puts(en)
		//trace.Trace("env var ", word, " is set to ", en) //<rmv/>
	}
	return scanToEnd
}

//─────────────┤ execFunc ├─────────────

func execFunc(t *template) scanfunc {
	cmd, _ := getToDelim(t, rightDelim)
	if len(cmd) > 0 {
		p := script.Exec(cmd)
		s, _ := p.String()
		s = strings.Trim(s, "\n")
		t.insert(s)
	}
	return scanText
}

//─────────────┤ fileFunc ├─────────────

func fileFunc(t *template) scanfunc {
	//trace := trace.New(os.Stdout) //<rmv/>
	//trace.Trace("entering fileFunc")//<rmv/>
	fn := getNextWord(t)
	//trace.Trace("include = ", fn) //<rmv/>
	_, _ = getToDelim(t, rightDelim)
	f, err := os.ReadFile(fn)
	if err != nil {
		return nil
	}
	t.insert(string(f))
	return scanText
}

//─────────────┤ letFunc ├─────────────

func letFunc(t *template) scanfunc {
	//var trace = trace.New(os.Stdout) //<rmv/>
	//trace.Trace("entering letFunc")  //<rmv/>
	v := getNextWord(t)
	//trace.Trace("var = ", v, " pos is ", t.Pos()) //<rmv/>
	val, _ := getToDelim(t, rightDelim)
	//trace.Trace("val = ", val) //<rmv/>
	val = strings.Trim(val, " ")
	//trace.Trace("let var ", v, " is set to ", val) //<rmv/>
	t.storeVar(v, val)
	return scanText
}

//─────────────┤ varFunc ├─────────────

func varFunc(t *template) scanfunc {
	//trace := trace.New(os.Stdout) //<rmv/>
	//trace.Trace("entering varFunc") //<rmv/>
	v := getNextWord(t)
	val := t.getVar(v)
	//trace.Trace("getVar returned *", val, "* for ", v) //<rmv/>
	if len(val) > 0 {
		t.puts(val)
	} else {
		t.puts(v)
	}
	return scanToEnd
}

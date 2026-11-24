package js

import (
	"io"
	"regexp"
	"testing"

	"github.com/tdewolff/parse/v2"
	"github.com/tdewolff/test"
)

func TestJS(t *testing.T) {
	var tests = []struct {
		js       string
		expected string
	}{
		{"if (true) { x(1, 2, 3); }", "if (true) { x(1, 2, 3); }"},
		{"if (true) { true; }", "if (true) { true; }"},
		{"if (true) { true; } else { false; }", "if (true) { true; } else { false; }"},
		{"if (true) { true; } else { if(true) { true; } else { false; } }", "if (true) { true; } else { if (true) { true; } else { false; } }"},
		{"do { continue; } while (true);", "do { continue; } while (true);"},
		{"do { x = 1; } while (true);", "do { x = 1; } while (true);"},
		{"while (true) { true; }", "while (true) { true; }"},
		{"while (true) { x = 1; }", "while (true) { x = 1; }"},
		{"for ( ; ; ) { true; }", "for ( ; ; ) { true; }"},
		{"for (x = 1; ; ) { true; }", "for (x = 1; ; ) { true; }"},
		{"for (x = 1; x < 2; ) { true; }", "for (x = 1; x < 2; ) { true; }"},
		{"for (x = 1; x < 2; x++) { true; }", "for (x = 1; x < 2; x++) { true; }"},
		{"for (x = 1; x < 2; x++) { x = 1; }", "for (x = 1; x < 2; x++) { x = 1; }"},
		{"for (var x in [1, 2]) { true; }", "for (var x in [1, 2]) { true; }"},
		{"for (var x in [1, 2]) { x = 1; }", "for (var x in [1, 2]) { x = 1; }"},
		{"for (const element of [1, 2]) { true; }", "for (const element of [1, 2]) { true; }"},
		{"for (const element of [1, 2]) { x = 1; }", "for (const element of [1, 2]) { x = 1; }"},
		{"switch (true) { case true: break; case false: false; }", "switch (true) { case true: break; case false: false; }"},
		{"switch (true) { case true: x(); break; case false: x(); false; }", "switch (true) { case true: x(); break; case false: x(); false; }"},
		{"switch (true) { default: false; }", "switch (true) { default: false; }"},
		{"for (i = 0; i < 3; i++) { continue; }", "for (i = 0; i < 3; i++) { continue; }"},
		{"for (i = 0; i < 3; i++) { x = 1; }", "for (i = 0; i < 3; i++) { x = 1; }"},
		{"function f(){return;}", "function f() { return; }"},
		{"function f(){return 1;}", "function f() { return 1; }"},
		{"with (true) { true; }", "with (true) { true; }"},
		{"with (true) { x = 1; }", "with (true) { x = 1; }"},
		{"loop: for (x = 0; x < 1; x++) { true; }", "loop: for (x = 0; x < 1; x++) { true; }"},
		{"throw x;", "throw x;"},
		{"try { true; } catch(e) { }", "try { true; } catch(e) {}"},
		{"try { true; } catch(e) { true; }", "try { true; } catch(e) { true; }"},
		{"try { true; } catch(e) { x = 1; }", "try { true; } catch(e) { x = 1; }"},
		{"debugger;", "debugger;"},
		{"import * as name from 'module-name';", "import * as name from 'module-name';"},
		{"import defaultExport from 'module-name';", "import defaultExport from 'module-name';"},
		{"import * as name from 'module-name';", "import * as name from 'module-name';"},
		{"import { export1 } from 'module-name';", "import { export1 } from 'module-name';"},
		{"import { export1 as alias1 } from 'module-name';", "import { export1 as alias1 } from 'module-name';"},
		{"import { export1 , export2 } from 'module-name';", "import { export1, export2 } from 'module-name';"},
		{"import { foo , bar } from 'module-name/path/to/specific/un-exported/file';", "import { foo, bar } from 'module-name/path/to/specific/un-exported/file';"},
		{"import defaultExport, * as name from 'module-name';", "import defaultExport, * as name from 'module-name';"},
		{"import 'module-name';", "import 'module-name';"},
		{"var promise = import('module-name');", "var promise = import('module-name');"},
		{"export { myFunction as default }", "export { myFunction as default };"},
		{"export default k = 12;", "export default k = 12;"},
		{"'use strict';", "'use strict';"},
		{"let [name1, name2 = 6] = z;", "let [name1, name2 = 6] = z;"},
		{"let {name1, key2: name2} = z;", "let {name1, key2: name2} = z;"},
		{"let [{name: key, ...rest}, ...[c,d=9]] = z;", "let [{name: key, ...rest}, ...[c, d = 9]] = z;"},
		{"var x;", "var x;"},
		{"var x = 1;", "var x = 1;"},
		{"var x, y = [];", "var x, y = [];"},
		{"let x;", "let x;"},
		{"let x = 1;", "let x = 1;"},
		{"const x = 1;", "const x = 1;"},
		{"function xyz (a, b) { }", "function xyz(a, b) {}"},
		{"function xyz (a, b, ...c) { }", "function xyz(a, b, ...c) {}"},
		{"function xyz (a, b) { }", "function xyz(a, b) {}"},
		{"class A { field; static get method () { } }", "class A { field; static get method () {} }"},
		{"class A { field; }", "class A { field; }"},
		{"class A { field = 5; }", "class A { field = 5; }"},
		{"class A { field; static get method () { } }", "class A { field; static get method () {} }"},
		{"class B extends A { field; static get method () { } }", "class B extends A { field; static get method () {} }"},

		{"x = 1;", "x = 1;"},
		{"'test';", "'test';"},
		{"[1, 2, 3];", "[1, 2, 3];"},
		{`x = {x: "value"};`, `x = {x: "value"};`},
		{`x = {"x": "value"};`, `x = {x: "value"};`},
		{`x = {"1a": 2};`, `x = {"1a": 2};`},
		{`x = {x: "value", y: "value"};`, `x = {x: "value", y: "value"};`},
		{"x = `value`;", "x = `value`;"},
		{"x = `value${'hi'}`;", "x = `value${'hi'}`;"},
		{"x = (1 + 1) / 1;", "x = (1 + 1) / 1;"},
		{"x = y[1];", "x = y[1];"},
		{"x = y.z;", "x = y.z;"},
		{"x = new.target;", "x = new.target;"},
		{"x = import.meta;", "x = import.meta;"},
		{"x(1, 2);", "x(1, 2);"},
		{"new x;", "new x();"},
		{"new x(1);", "new x(1);"},
		{"new Date().getTime();", "new Date().getTime();"},
		{"x();", "x();"},
		{"x = y?.z;", "x = y?.z;"},
		{"x = -a;", "x = -a;"},
		{"x = - --a;", "x = - --a;"},
		{"a << b;", "a << b;"},
		{"a && b;", "a && b;"},
		{"a || b;", "a || b;"},
		{"x = function* foo (x) { while (x < 2) { yield x; x++; } };", "x = function* foo(x) { while (x < 2) { yield x; x++; } };"},
		{"(x) => { y(); };", "(x) => { y(); };"},
		{"(x, y) => { z(); };", "(x, y) => { z(); };"},
		{"async (x, y) => { z(); };", "async (x, y) => { z(); };"},
		{"await x;", "await x;"},
		{"export default await x;", "export default await x;"},
		{"export let a = await x;", "export let a = await x;"},
		{"if(k00)while((0))", "if (k00) while ((0));"},
		{"export{};from", "export {}; from;"},
		{"import{} from 'a'", "import {} from 'a';"},
		{"import o,{} from''", "import o, {} from '';"},
		{"if(0)var s;else", "if (0) var s; else;"},
		{"async\n()", "async();"},
		{"{};;", "{} ;"},
		{"{}\n;", "{} ;"},
		{"- - --3", "- - --3;"},
		{"([,,])=>P", "([,,]) => { return P; };"},
		{"(t)=>{//!\n}", "(t) => { //! };"}, // space after //! is newline
		{"import();", "import();"},
		{"0\n.k", "(0).k;"},
		{"do//!\n; while(1)", "//! do; while (1);"},           // space after //! is newline
		{"//!\nn=>{ return n }", "//! (n) => { return n; };"}, // space after //! is newline
		{"//!\n{//!\n}", "//! { //! }"},                       // space after //! is newline
		{`for(;;)let = 5`, `for ( ; ; ) { (let = 5); }`},
		{"{`\n`}", "{ ` `; }"}, // space in template literal is newline
		{"import.meta;", "import.meta;"},
	}

	re := regexp.MustCompile("\n *")
	for _, tt := range tests {
		t.Run(tt.js, func(t *testing.T) {
			ast, err := Parse(parse.NewInputString(tt.js), Options{})
			if err != io.EOF {
				test.Error(t, err)
			}
			src := ast.JSString()
			src = re.ReplaceAllString(src, " ")
			test.String(t, src, tt.expected)
		})
	}
}

func TestJSON(t *testing.T) {
	input := `[{"key": [2.5, '\r'], '"': -2E+9}, null, false, true, 5.0e-6, "string", 'stri"ng']`
	ast, err := Parse(parse.NewInputString(input), Options{})
	if err != nil {
		t.Fatal(err)
	}
	json, err := ast.JSONString()
	if err != nil {
		t.Fatal(err)
	}
	test.String(t, json, `[{"key": [2.5, "\r"], "\"": -2E+9}, null, false, true, 5.0e-6, "string", "stri\"ng"]`)
}

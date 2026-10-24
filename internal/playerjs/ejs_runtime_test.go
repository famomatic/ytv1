package playerjs

import "testing"

// Minimal stand-in for the modern player layout: a URL-builder function that
// calls .set("alr","yes") and a prototype method that rewrites `s` and `n`.
const ejsFixturePlayer = `var _yt_player={};(function(g){
var Fl=function(u){this.p={};};
Fl.prototype.set=function(k,v){this.p[k]=v;};
Fl.prototype.get=function(k){return this.p[k];};
Fl.prototype.decode=function(){
  if(this.p.s!==undefined){this.p.s=decodeURIComponent(this.p.s).split("").reverse().join("");}
  if(this.p.n!==undefined){this.p.n=this.p.n+"X";}
};
Hl=function(a,b="",c=""){a=new Fl(a);a.set("alr","yes");c&&a.set(b,c);return a};
g.other=function(){};
})(_yt_player);`

func TestDecipherRuntimeEJSSolver(t *testing.T) {
	d := NewDecipherer(ejsFixturePlayer)
	got, err := d.DecipherSignature("abc")
	if err != nil || got != "cba" {
		t.Fatalf("DecipherSignature = %q, %v; want cba", got, err)
	}
	n, err := d.DecipherN("foo")
	if err != nil || n != "fooX" {
		t.Fatalf("DecipherN = %q, %v; want fooX", n, err)
	}
}

func TestSafeFunctionBlocksConstructor(t *testing.T) {
	player := `var _yt_player={};(function(g){
var r="none";
try{(function(){}).constructor("return 1")();r="escaped";}catch(e){r="blocked";}
g.res=r;
Hl=function(a,b,c){a.set("alr","yes")};
})(_yt_player);`
	d := NewDecipherer(player)
	rt, err := d.loadRuntimeDecipherer()
	if err != nil {
		t.Fatal(err)
	}
	v := rt.vm.Get("_yt_player").ToObject(rt.vm).Get("res")
	if v == nil || v.String() != "blocked" {
		t.Fatalf("Function constructor not blocked: %v", v)
	}
}

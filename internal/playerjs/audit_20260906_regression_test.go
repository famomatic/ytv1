package playerjs

import "testing"

func TestAudit20260906InvalidNReturnsNilError(t *testing.T) {
	js := `var _yt_player={};(function(g){
 NN=function(a){return a};
 probe=function(x){x.get("n")&&(b=NN(b))};
 g.Mj=function(u){this.get=function(k){return "same"}};
 MzD=function(b){try{const R=(new g.Mj(b,!0)).get("n");}catch(R){}return b};
 })(_yt_player);`
	d := NewDecipherer(js)
	got, e := d.DecipherN("abcdef")
	if e == nil || got != "" {
		t.Fatalf("regression: %q %v", got, e)
	}

}

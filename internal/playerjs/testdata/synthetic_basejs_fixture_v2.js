var XY={rv:function(a){a.reverse()},sp:function(a,b){a.splice(0,b)},sw:function(a,b){var c=a[0];a[0]=a[b%a.length];a[b]=c;return a}};
GH=function(a){a=a.split("");a=XY["sw"](a,3);a=XY["rv"](a,0);a=XY["sp"](a,2);return a.join("")}
foo.get("n"))&&(b=Nt(b))
Nt = function(a){a=a.split("");a.splice(0,2);return a.join("")}

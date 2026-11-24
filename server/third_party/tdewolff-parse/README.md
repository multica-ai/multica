# tdewolff/parse JavaScript parser snapshot

The root parser package, buffer package, and JavaScript parser come from
[tdewolff/parse v2.8.16](https://github.com/tdewolff/parse/tree/b8093418813ea76128e967af71a9be510cff1447)
under the included MIT license. The snapshot includes a small change in
`js/parse.go` to let the plugin validator parse class field initializers
in the context used by classic scripts. The validator opts in explicitly.

Use the pinned source and local module replacement together. A compatible
upstream release can replace the snapshot once the dual-entry regressions
pass against that release.

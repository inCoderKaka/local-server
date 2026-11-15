// Package webui is the webui module (docs/modules/webui.md): it serves a built single-page web
// UI (the admin UI or the consumer web page) from an fs.FS with a strict Content Security
// Policy on every answer (req_001 D-10, D-21, D-151, D-152).
//
// # Surface
//
// Handler(Config) returns an http.Handler that answers GET and HEAD only (any other method is
// 405 with Allow). It serves the files of Config.Files by their cleaned path, never a directory
// listing, never a dot file, and never a path that leaves the file system. A path that is not a
// file and has no extension in its last segment is answered with index.html, so a client-side
// route loads the app; a path with an extension that is not a file is 404.
//
// Every answer, the 404 and 405 included, carries the CSP (CSP), nosniff, no-referrer and a
// same-origin resource policy, and no CORS header: the UI calls only its own origin. Files are
// answered with Cache-Control no-cache and an ETag that is a hash of the content, so a browser
// revalidates every time and gets 304 while the file is unchanged; no cache lifetime is written
// down here (req_001 D-38).
//
// When Config.Files has no index.html (the UI was not built: git holds only dist/.gitkeep,
// req_001 D-152) every page path is answered with a committed one-page stub that says how to
// build the UI. The stub has no script and no style, so it passes the same CSP.
package webui

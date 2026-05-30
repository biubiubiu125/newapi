//go:build !webdist

package main

import "embed"

var buildFS embed.FS
var indexPage = []byte("<!doctype html><html><head><title>New API</title></head><body>frontend assets are not embedded in this build</body></html>")

var classicBuildFS embed.FS
var classicIndexPage = []byte("<!doctype html><html><head><title>New API</title></head><body>frontend assets are not embedded in this build</body></html>")

package web

import "embed"

//go:embed static/* templates/*
var Assets embed.FS

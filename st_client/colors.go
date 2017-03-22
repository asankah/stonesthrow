package main

import (
	"github.com/mgutz/ansi"
)

var (
	CError           = ansi.ColorFunc("1")
	CSucceeded       = ansi.ColorFunc("40")
	CInfo            = ansi.ColorFunc("6")
	CDark            = ansi.ColorFunc("238")
	CTitle           = ansi.ColorFunc("15+b")
	CSubject         = ansi.ColorFunc("31")
	CLocation        = ansi.ColorFunc("3")
	CSourceBuildStep = ansi.ColorFunc("34")
	CAuxBuildStep    = ansi.ColorFunc("214")
	CActionLabel     = ansi.ColorFunc("34")
)

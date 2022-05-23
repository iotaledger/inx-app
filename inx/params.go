package inx

import (
	"github.com/iotaledger/hive.go/app"
)

type ParametersINX struct {
	Address string `default:"localhost:9029" usage:"the INX address to which to connect to"`
}

var ParamsINX = &ParametersINX{}

var params = &app.ComponentParams{
	Params: map[string]any{
		"inx": ParamsINX,
	},
	Masked: nil,
}

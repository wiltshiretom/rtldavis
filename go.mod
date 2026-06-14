module github.com/wiltshiretom/rtldavis

go 1.26.2

require (
	github.com/jpoirier/gortlsdr v2.10.0+incompatible
	github.com/lheijst/rtldavis v0.0.0-20200605114342-b95d5d734e46
)

// Use local fork's protocol/dsp/crc packages instead of upstream lheijst module
replace github.com/lheijst/rtldavis => ./

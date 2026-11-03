//go:build amd64

#include "textflag.h"

// func Ticks() uint64
TEXT ·Ticks(SB), NOSPLIT, $0-8
	RDTSCP
	SHLQ $32, DX
	ORQ  DX, AX
	MOVQ AX, ret+0(FP)
	RET

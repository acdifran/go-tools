package utils

import "runtime"

func Last[E any](s []E) (E, bool) {
	if len(s) == 0 {
		var zero E
		return zero, false
	}
	return s[len(s)-1], true
}

func GetFuncName() string {
	pc := make([]uintptr, 1)
	runtime.Callers(2, pc)

	frames := runtime.CallersFrames(pc)
	frame, _ := frames.Next()
	return frame.Function
}

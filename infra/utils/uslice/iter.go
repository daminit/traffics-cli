package uslice

func Unique[E comparable, S ~[]E](s S) S {
	if len(s) == 0 {
		return s
	}
	dis := make(map[E]bool)
	ret := make(S, 0)
	for _, v := range s {
		if _, exist := dis[v]; !exist {
			ret = append(ret, v)
		}
	}
	return ret
}

package playerjs

func newSpliceFunc(pos int) DecipherOperation {
	return func(bs []byte) []byte {
		if pos < 0 || pos > len(bs) {
			return bs
		}
		return bs[pos:]
	}
}

func newSwapFunc(arg int) DecipherOperation {
	return func(bs []byte) []byte {
		if len(bs) == 0 {
			return bs
		}
		pos := arg % len(bs)
		bs[0], bs[pos] = bs[pos], bs[0]
		return bs
	}
}

func reverseFunc(bs []byte) []byte {
	l, r := 0, len(bs)-1
	for l < r {
		bs[l], bs[r] = bs[r], bs[l]
		l++
		r--
	}
	return bs
}


package plex

var V int

func Single() int {
	defer func() { V++ }()
	return 1
}

func Multiple() int {
	defer func() { V += 3 }()
	return 3
}

func Dead() int {
	defer func() { V -= 1 }()
	return -1
}

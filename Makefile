bench-nonce:
	go get github.com/cosmos/iavl@329f89d1552dd128d6552af5008dfe4f4f927abb
	go test -run=NOTEST -bench=. -timeout=600m >> bench-nonce.txt
.PHONY: bench-nonce

bench-path:
	go get github.com/cosmos/iavl@61edd1d678d8ffe4f71e0ae845475a2f97c9e37e
	go test -run=NOTEST -bench=. -timeout=600m >> bench-path.txt
.PHONY: bench-path

bench-master:
	go get github.com/cosmos/iavl@master
	go test -run=NOTEST -bench=. -timeout=600m >> bench-master.txt
.PHONY: bench-master

bench: bench-nonce bench-path bench-master
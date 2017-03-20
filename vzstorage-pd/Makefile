BINS	:= vzstorage-pd
BINDIR	:= /usr/bin

all:
	go build .
.PHONY: all

install-bin:
	install -d $(DESTDIR)$(BINDIR)
	install $(BINS) $(DESTDIR)$(BINDIR)/
.PHONY: install-bin

install: install-bin
.PHONY: install

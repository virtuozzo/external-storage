NAME	:= vzstorage-pd
BINS	:= vzstorage-pd
BINDIR	:= /usr/bin
GITID	?= $(shell git describe --always HEAD || true)
TARNAME	:= $(NAME)-$(GITID)

all:
	go build .
.PHONY: all

tar: $(TARNAME).tar.bz2
.PHONY: tar

dist: tar
.PHONY: dist

$(TARNAME).tar.bz2:
	(cd .. && git archive --format tar --prefix "$(TARNAME)/" $(GITID)) \
		| bzip2 -9 > $@

install-bin:
	install -d $(DESTDIR)$(BINDIR)
	install $(BINS) $(DESTDIR)$(BINDIR)/
.PHONY: install-bin

install: install-bin
.PHONY: install

clean:
	rm -f $(BINS)
	rm -f $(TARNAME).tar.bz2
.PHONY: clean

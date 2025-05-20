FIO_SRC_ROOT =? /you/must/set/FIO_SRC_ROOT=path/on/the/commandline/
FIO_CONFIG_HOST = $(FIO_SRC_ROOT)/config-host.h

GODEPS = storagewrapper/*.go storagewrapper/go.mod storagewrapper/go.sum
GOARCHIVE = storagewrapper/storagewrapper.a
GOOUTS = storagewrapper/storagewrapper.h $(GOARCHIVE)
ENGINE_SRC = go-storage-fio-engine.c
TARGET = libgo-storage-fio-engine.so

CC = gcc
CFLAGS = -include $(FIO_CONFIG_HOST) -I./ -I$(FIO_SRC_ROOT) -O2 -g -D_GNU_SOURCE -fPIC
LDFLAGS = -shared -rdynamic

# ---------------------------------------------------------------------------

.PHONY: all clean

all: $(TARGET)

$(FIO_CONFIG_HOST): $(FIO_SRC_ROOT)/configure
	env -C $(FIO_SRC_ROOT) ./configure

$(GOOUTS): $(GODEPS)
	env -C storagewrapper/ go build -buildmode=c-archive ./

$(TARGET): $(ENGINE_SRC) $(FIO_CONFIG_HOST) $(GOOUTS)
	$(CC) $(CFLAGS) $(LDFLAGS) $(ENGINE_SRC) $(GOARCHIVE) -o $(TARGET)

clean:
	rm -f $(TARGET) $(GOOUTS)


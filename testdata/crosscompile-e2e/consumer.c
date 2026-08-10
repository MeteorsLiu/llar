#include <stdio.h>
#include <string.h>
#include <zlib.h>

int main(void) {
    static const Bytef input[] = "LLAR cross-compiled zlib";
    Bytef compressed[128];
    Bytef restored[128];
    uLongf compressed_len = sizeof(compressed);
    uLongf restored_len = sizeof(restored);

    if (compress(compressed, &compressed_len, input, sizeof(input)) != Z_OK) {
        return 1;
    }
    if (uncompress(restored, &restored_len, compressed, compressed_len) != Z_OK) {
        return 2;
    }
    if (restored_len != sizeof(input) || memcmp(restored, input, sizeof(input)) != 0) {
        return 3;
    }
    if (strcmp(zlibVersion(), ZLIB_VERSION) != 0) {
        return 4;
    }

    printf("zlib %s: compressed %lu bytes to %lu bytes\n",
        zlibVersion(), (unsigned long)sizeof(input), (unsigned long)compressed_len);
    return 0;
}

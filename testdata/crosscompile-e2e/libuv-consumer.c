#include <stdio.h>

#include <uv.h>

#ifndef EXPECT_ABI_MATCH
#error EXPECT_ABI_MATCH must be defined
#endif

int main(void) {
    size_t built_size = uv_loop_size();
    size_t consumer_size = sizeof(uv_loop_t);
    int matches = built_size == consumer_size;

    printf("libuv loop size: library=%zu consumer=%zu\n",
        built_size, consumer_size);
    if (matches != EXPECT_ABI_MATCH) {
        return 1;
    }
    return 0;
}

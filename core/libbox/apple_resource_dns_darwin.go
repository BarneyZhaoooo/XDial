//go:build darwin && cgo

package libbox

/*
#cgo LDFLAGS: -lresolv
#include <arpa/inet.h>
#include <netdb.h>
#include <resolv.h>

// The compiler uses the first OS-provided resolver for endpoint bootstrap.
static int firstResolver(char *host, size_t size) {
 struct __res_state state = {0};
 if (res_ninit(&state) != 0) return -1;
 union res_sockaddr_union first = {0};
 int count = res_getservers(&state, &first, 1);
 res_ndestroy(&state);
 if (count < 1) return -1;
 const struct sockaddr *address = (const struct sockaddr *)&first;
 return getnameinfo(address, address->sa_len, host, (socklen_t)size, NULL, 0, NI_NUMERICHOST);
}
*/
import "C"

func appleResourceDNS() []string {
	var host [C.NI_MAXHOST]C.char
	if C.firstResolver(&host[0], C.size_t(len(host))) != 0 {
		return nil
	}
	return []string{C.GoString(&host[0])}
}

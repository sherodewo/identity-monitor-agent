package extractor

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestNormalize(t *testing.T) {
	u, e := URL("https://EXAMPLE.org/page?utm_source=x&b=2&a=1#section")
	require.NoError(t, e)
	require.Equal(t, "https://example.org/page?a=1&b=2", u)
	require.Equal(t, Hash("Hello WORLD"), Hash(" hello \n world "))
	require.NotEqual(t, Hash("hello"), Hash("world"))
	for _, u := range []string{"file:///secret", "javascript:alert(1)", "https://user:pass@example.org"} {
		_, e = URL(u)
		require.Error(t, e)
	}
}

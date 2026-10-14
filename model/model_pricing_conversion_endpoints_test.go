package model

import "testing"

import "github.com/stretchr/testify/require"

func TestDeclaredEndpointNamesAcceptsArrayAndMap(t *testing.T) {
	names, err := declaredEndpointNames(`[" image-generation ", "openai"]`)
	require.NoError(t, err)
	require.Equal(t, []string{"image-generation", "openai"}, names)

	names, err = declaredEndpointNames(`{" image-generation ":"/v1/images/generations"}`)
	require.NoError(t, err)
	require.Equal(t, []string{"image-generation"}, names)

	names, err = declaredEndpointNames("   ")
	require.NoError(t, err)
	require.Nil(t, names)

	_, err = declaredEndpointNames(`[1]`)
	require.Error(t, err)
	_, err = declaredEndpointNames(`["image-generation", ""]`)
	require.Error(t, err)
	_, err = declaredEndpointNames(`"openai"`)
	require.Error(t, err)
}

// Copyright (C) 2019 Storj Labs, Inc.
// See LICENSE for copying information.

#include <string.h>
#include <stdlib.h>

#include "require.h"

#include "uplink.h"

int main(int argc, char *argv[])
{
    char *_err = "";
    char **err = &_err;

    char *scopeStr = "1ZYMge4erhJ7hSTf4UCUvtcT2e7rHBNrQvVMgxVDPgFwndj2f2tUnoqmQhaQapEvkifiu9Dwi53C8a3QKB8xMYPZkKS3yCLKbhaccpRg91iDGJuUBS7m7FKW2AmvQYNm5EM56AJrCsb95CL4jTd686sJmuGMnpQhd6NqE7bYAsQTCyADUS15kDJ2zBzt43k689TwW";
    {
        ScopeRef scope = parse_scope(scopeStr, err);
        require_noerror(*err);
        requiref(scope._handle != 0, "got empty scope\n");

        char *scopeSerialized = serialize_scope(scope, err);
        require_noerror(*err);

        requiref(strcmp(scopeSerialized, scopeStr) == 0,
                 "got invalid serialized %s expected %s\n", scopeSerialized, scopeStr);

        free_scope(scope);
    }

    requiref(internal_UniverseIsEmpty(), "universe is not empty\n");
}
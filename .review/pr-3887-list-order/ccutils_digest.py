#!/usr/bin/env python3
"""Print cc-utils' jsonNormalisation/v1 bytes and digest for a component descriptor.

Usage: CCUTILS_DIR=<cc-utils checkout> ccutils_digest.py <descriptor.yaml> [--digest]

Uses gardener/cc-utils ocm/sign.py unchanged as the oracle (the one the PR names).
Digest lookups are disabled: normalisation runs offline on persisted digests only.
"""
import json
import os
import sys

import yaml

sys.path.insert(0, os.environ["CCUTILS_DIR"])
import ocm  # noqa: E402
import ocm.sign  # noqa: E402


def no_lookup(*_args, **_kwargs):
    raise RuntimeError("oracle must not resolve digests")


with open(sys.argv[1]) as f:
    cd = ocm.ComponentDescriptor.from_dict(yaml.safe_load(f))

if "--digest" in sys.argv:
    print(ocm.sign.component_descriptor_digest(cd, no_lookup, no_lookup))
else:
    norm = ocm.sign.normalise_component_descriptor(cd, no_lookup, no_lookup)
    sys.stdout.write(json.dumps(obj=norm, separators=(",", ":")))

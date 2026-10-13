import json, sys
import ocm, ocm.sign

def no_lookup(*a, **k):
    raise RuntimeError('lookup must not be needed')

d = json.load(open(sys.argv[1]))
cd = ocm.ComponentDescriptor.from_dict(d)
n = ocm.sign.normalise_component_descriptor(
    component_descriptor=cd,
    component_descriptor_lookup=no_lookup,
    access_to_digest_lookup=no_lookup,
)
sys.stdout.write(json.dumps(obj=n, separators=(',', ':')))

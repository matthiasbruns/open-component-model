import json, sys, copy
out = sys.argv[1]
def res(name='img', ver='1.0.0', ei=None, labels=None, dig='a'*64, access=None):
    return {"name": name, "version": ver, "type": "ociImage",
            "access": access or {"type": "ociRegistry", "imageReference": "registry.example/img@sha256:aa"},
            "digest": {"hashAlgorithm": "SHA-256", "normalisationAlgorithm": "ociArtifactDigest/v1", "value": dig},
            "extraIdentity": ei or {}, "relation": "external", "labels": labels or [], "srcRefs": []}
def cd(resources=None, provider="ACME", labels=None, ctime=None):
    return {"meta": {"schemaVersion": "v2"}, "component": {"name": "acme.org/case", "version": "1.0.0",
            "repositoryContexts": [], "provider": provider, "sources": [], "componentReferences": [],
            "resources": resources if resources is not None else [res()], "labels": labels or [], "creationTime": ctime},
            "signatures": [], "nestedDigests": []}
def lbl(name, value, version=None):
    return {"name": name, "value": value, "version": version, "signing": True}
cases = {
  "00-baseline": cd(),
  "01-label-list-of-objects": cd(labels=[lbl("cfg", [{"b": "1", "a": "2"}])]),
  "02-label-html-chars": cd(labels=[lbl("url", "https://x.example/?a=1&b=<2>")]),
  "03-non-ascii-provider": cd(provider="Müller GmbH"),
  "04-non-ascii-label": cd(labels=[lbl("owner", "José")]),
  "05-label-float": cd(labels=[lbl("ratio", 1.0)]),
  "06-label-bigint": cd(labels=[lbl("id", 12345678901234567890)]),
  "07-ctime-non-utc-offset": cd(ctime="2024-01-01T10:00:00+02:00"),
  "08-dup-explicit-version-peer": cd(resources=[res(ei={}), res(ei={"version": "2.0.0"}, dig='b'*64)]),
  "09-dup-delimiter-collision": cd(resources=[res(name="res", ei={"aa": "b;cc=dd"}), res(name="res", ei={"aa": "b", "cc": "dd"}, dig='b'*64)]),
  "10-dup-three": cd(resources=[res(), res(dig='b'*64), res(dig='c'*64)]),
}
for k, v in cases.items():
    json.dump(v, open(f"{out}/{k}.json", "w"))
print(len(cases))

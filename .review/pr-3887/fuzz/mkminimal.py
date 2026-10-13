import json, sys, os, copy
out = sys.argv[1]
D = 'a' * 64
def res(name='img', ver='1.0.0', ei=None, access=None, digest=True):
    r = {'name': name, 'version': ver, 'type': 'ociImage', 'relation': 'external',
         'access': access or {'type': 'ociRegistry', 'imageReference': 'r.example/img:1'}}
    if digest: r['digest'] = {'hashAlgorithm': 'SHA-256', 'normalisationAlgorithm': 'ociArtifactDigest/v1', 'value': D}
    if ei is not None: r['extraIdentity'] = ei
    return r
def cd(resources=None, provider='ACME', labels=None, refs=None):
    c = {'name': 'acme.org/c', 'version': '1.0.0', 'provider': provider, 'repositoryContexts': [], 'sources': [],
         'resources': resources if resources is not None else [res()], 'componentReferences': refs or []}
    if labels: c['labels'] = labels
    return {'meta': {'schemaVersion': 'v2'}, 'component': c}
cases = {
 'c1-html-escape': cd(labels=[{'name': 'url', 'value': 'https://x/?a=1&b=<2>', 'signing': True}]),
 'c2-none-access-with-digest': cd(resources=[res(access={'type': 'none'})]),
 'c3-reference-without-digest': cd(refs=[{'name': 'child', 'componentName': 'acme.org/child', 'version': '1.0.0'}]),
 'c4a-provider-nonsigning-label': cd(provider=json.dumps({'name': 'ACME', 'labels': [{'name': 'team', 'value': 'x'}]})),
 'c4b-provider-json-no-labels': cd(provider=json.dumps({'name': 'ACME', 'labels': []})),
 'c5a-dup-peer-explicit-version': cd(resources=[res(ei={}), res(ei={'version': '2.0.0'})]),
 'c5b-dup-delimiter-collision': cd(resources=[res(ei={'a': 'b;c=d'}), res(ei={'a': 'b', 'c': 'd'})]),
 'c5c-dup-overwrite-existing-version': cd(resources=[res(ver='2.0.0', ei={'version': '1.0.0'}), res(ver='1.0.0', ei={'version': '1.0.0'})]),
 'c6-provider-label-string-signing': cd(provider=json.dumps({'name': 'ACME', 'labels': [{'name': 'team', 'value': 'x', 'signing': 'true'}]})),
}
for k, v in cases.items():
    json.dump(v, open(os.path.join(out, 'c-' + k + '.json'), 'w'))

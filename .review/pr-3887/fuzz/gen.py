"""Seeded generator of small v2 component descriptors for differential testing.
usage: python3 -I gen.py <outdir> <count> [seed]"""
import json, random, sys, os

out, count = sys.argv[1], int(sys.argv[2])
seed = int(sys.argv[3]) if len(sys.argv) > 3 else 3887
R = random.Random(seed)
os.makedirs(out, exist_ok=True)

SPECIAL = ['&', '<', '>', ' ', ' ', '\x7f', '\U0001F600', '"', '\\', 'ü', 'é', '€', '中',
           '\t', '\n', '\x01', ';', '=', '/', "'", ' ', ' ', '﻿']
def s(base='x', p=0.3):
    v = base
    if R.random() < p:
        for _ in range(R.randint(1, 3)):
            i = R.randint(0, len(v)); v = v[:i] + R.choice(SPECIAL) + v[i:]
    return v

NUMS = [0, 1, -1, 1.0, 1e21, 1e20, -0.0, 0.1, 1.5e-7, 12345678901234567890, 2**53 + 1, 3.14, 100.0, 1e-5, 123456789012]
def value(depth=0):
    k = R.randint(0, 9 if depth < 2 else 4)
    if k == 0: return s('val', 0.6)
    if k == 1: return R.choice(NUMS)
    if k == 2: return R.choice([True, False])
    if k == 3: return None
    if k == 4: return s('v', 0.2)
    if k == 5: return {s('k%d' % i, 0.3): value(depth + 1) for i in range(R.randint(1, 3))}
    if k == 6: return [value(depth + 1) for _ in range(R.randint(1, 3))]
    if k == 7: return [{s('b', 0.2): value(depth + 1), s('a', 0.2): value(depth + 1)}]
    if k == 8: return R.choice([{}, []])
    return {'nested': {'z': value(depth + 1), 'a': [1, {'y': 'q', 'x': 'p'}]}}

def label(i, allow_str_signing=False):
    l = {'name': s('lbl%d' % i, 0.3), 'value': value()}
    sg = R.choice(['true', 'false', 'absent', 'true'] + (['strtrue'] if allow_str_signing else []))
    if sg == 'true': l['signing'] = True
    elif sg == 'false': l['signing'] = False
    elif sg == 'strtrue': l['signing'] = 'true'
    if R.random() < 0.4: l['version'] = R.choice(['v1', 'v2', 'v10'])
    return l

def labels(allow_str_signing=False):
    return [label(i, allow_str_signing) for i in range(R.randint(0, 3))]

def digest(kind='oci'):
    if kind == 'excl':
        return {'hashAlgorithm': 'NO-DIGEST', 'normalisationAlgorithm': 'EXCLUDE-FROM-SIGNATURE', 'value': 'NO-DIGEST'}
    return {'hashAlgorithm': 'SHA-256', 'normalisationAlgorithm': R.choice(['ociArtifactDigest/v1', 'genericBlobDigest/v1']),
            'value': ''.join(R.choice('0123456789abcdef') for _ in range(64))}

def extra_identity(allow_delim=True):
    c = R.random()
    if c < 0.35: return 'absent'
    if c < 0.5: return {}
    if c < 0.55: return None
    ei = {}
    for i in range(R.randint(1, 2)):
        ei[s('ek%d' % i, 0.1)] = s('ev', 0.4 if allow_delim else 0.0)
    if R.random() < 0.2: ei['version'] = R.choice(['1.0.0', '2.0.0'])
    return ei

def resource(name, version):
    acc_t = R.choice(['ociRegistry'] * 6 + ['localBlob', 'none', 'None', 's3'])
    access = {'type': acc_t}
    if acc_t == 'ociRegistry': access['imageReference'] = 'r.example/img:1'
    if acc_t == 'localBlob': access.update({'localReference': 'sha256:' + 'a' * 64, 'mediaType': 'application/octet-stream'})
    r = {'name': name, 'version': version, 'type': R.choice(['ociImage', 'helmChart', 'blob', s('t', 0.2)]),
         'relation': R.choice(['local', 'external']), 'access': access}
    d = R.random()
    if d < 0.75: r['digest'] = digest()
    elif d < 0.85: r['digest'] = digest('excl')
    ei = extra_identity()
    if ei != 'absent': r['extraIdentity'] = ei
    if R.random() < 0.5: r['labels'] = labels()
    if R.random() < 0.2: r['srcRefs'] = [{'identitySelector': {'name': 'src'}, 'labels': labels()}]
    return r

def resources():
    res = []
    n = R.randint(0, 3)
    for i in range(n):
        res.append(resource(s('res%d' % i, 0.2), R.choice(['1.0.0', '1.0.1', '2.0.0'])))
    if R.random() < 0.5:  # duplicates
        base = resource(s('dup', 0.3), '1.0.0')
        for k in range(R.randint(2, 4)):
            r = json.loads(json.dumps(base))
            r['version'] = R.choice(['1.0.0', '1.0.%d' % k, '2.0.0'])
            r['digest'] = digest()
            if R.random() < 0.3:
                ei = r.get('extraIdentity') or {}
                if not isinstance(ei, dict): ei = {}
                ei = dict(ei); ei['version'] = R.choice([r['version'], '9.9.9']); r['extraIdentity'] = ei
            if R.random() < 0.15:
                r['extraIdentity'] = extra_identity()
                if r['extraIdentity'] == 'absent': del r['extraIdentity']
            res.insert(R.randint(0, len(res)), r)
    return res

def reference(i):
    ref = {'name': s('ref%d' % i, 0.2), 'componentName': 'acme.org/' + s('child', 0.1), 'version': '1.0.0'}
    if R.random() < 0.8:
        ref['digest'] = {'hashAlgorithm': 'SHA-256', 'normalisationAlgorithm': 'jsonNormalisation/v1', 'value': 'c' * 64}
    ei = extra_identity()
    if ei != 'absent': ref['extraIdentity'] = ei
    if R.random() < 0.4: ref['labels'] = labels()
    return ref

CTIMES = [None, '2024-01-01T10:00:00Z', '2024-01-01T10:00:00+02:00', '2024-01-01T10:00:00.5Z',
          '2024-01-01T10:00:00.499999Z', '2024-01-01T10:00:00.123456789-07:30', '2025-06-20T11:25:07.949163+00:00',
          '2024-02-29T23:59:59.9Z']
def provider():
    c = R.random()
    if c < 0.6: return s('ACME', 0.4)
    p = {'name': s('ACME', 0.4)}
    if R.random() < 0.7: p['labels'] = labels(allow_str_signing=True)
    return json.dumps(p, ensure_ascii=R.random() < 0.5)  # OCM v1 v2 schema stores a structured provider as a JSON string

def cd():
    comp = {'name': 'acme.org/' + s('comp', 0.1), 'version': '1.0.0', 'provider': provider(),
            'repositoryContexts': R.choice([[], [{'type': 'OCIRegistry', 'baseUrl': 'ghcr.io', 'subPath': 'x', 'componentNameMapping': 'urlPath'}]]),
            'resources': resources(), 'componentReferences': [reference(i) for i in range(R.randint(0, 2))]}
    if R.random() < 0.3: comp['sources'] = [{'name': 'src', 'version': '1.0.0', 'type': 'git', 'access': {'type': 'gitHub', 'repoUrl': 'github.com/a/b', 'commit': 'abc'}}]
    else: comp['sources'] = []
    if R.random() < 0.5: comp['labels'] = labels()
    ct = R.choice(CTIMES)
    if ct is not None or R.random() < 0.5: comp['creationTime'] = ct
    d = {'meta': {'schemaVersion': 'v2'}, 'component': comp}
    if R.random() < 0.2:
        d['signatures'] = [{'name': 'sig', 'digest': {'hashAlgorithm': 'SHA-256', 'normalisationAlgorithm': 'jsonNormalisation/v1', 'value': 'd' * 64},
                            'signature': {'algorithm': 'RSASSA-PKCS1-V1_5', 'value': 'abcd', 'mediaType': 'application/vnd.ocm.signature.rsa'}}]
    if R.random() < 0.1: d['nestedDigests'] = []
    return d

for i in range(count):
    with open(os.path.join(out, 'c%05d.json' % i), 'w', encoding='utf-8') as f:
        txt = json.dumps(cd(), ensure_ascii=False)
        # YAML (which OCM v1 decodes through) rejects raw DEL/NEL/BOM/line separators and surrogate escapes
        for ch in ['\x7f', '\x85', '\u2028', '\u2029', '\ufeff', '\u00a0']:
            txt = txt.replace(ch, '\\u%04x' % ord(ch))
        f.write(txt)
print(count)

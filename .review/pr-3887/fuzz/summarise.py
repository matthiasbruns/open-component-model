import sys, os, glob, collections, json
d = sys.argv[1]
st = collections.Counter(); errs = collections.Counter(); div = []
for f in sorted(glob.glob(os.path.join(d, 'c*.json'))):
    pr = open(f + '.pr', encoding='utf-8', errors='replace').read()
    v1 = open(f + '.v1', encoding='utf-8', errors='replace').read()
    pe, ve = pr.startswith('ERROR'), v1.startswith('ERROR')
    if ve and 'DECODE' in v1: st['v1-decode-rejected'] += 1; errs['v1 decode: ' + v1[:110]] += 1; continue
    if pe and ve: st['both-error'] += 1; errs['both: PR ' + pr[:70] + ' | V1 ' + v1[:70]] += 1
    elif pe: st['pr-error-only'] += 1; errs['PR only: ' + pr[:140]] += 1
    elif ve: st['v1-error-only'] += 1; errs['V1 only: ' + v1[:140]] += 1
    elif pr == v1: st['agree'] += 1
    else: st['diverge'] += 1; div.append(os.path.basename(f))
print(dict(st))
for k, v in errs.most_common(20): print(v, k)
open(os.path.join(d, '..', 'diverging.txt'), 'w').write('\n'.join(div))

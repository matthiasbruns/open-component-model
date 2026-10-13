"""usage: cmp.py <casedir> <suffix>  -> agree/diverge counts vs OCM v1 (.v1), excluding v1-rejected"""
import sys, os, glob, collections
d, suf = sys.argv[1], sys.argv[2]
c = collections.Counter(); div = []
for f in sorted(glob.glob(os.path.join(d, 'c*.json'))):
    v1 = open(f + '.v1', encoding='utf-8').read(); pr = open(f + suf, encoding='utf-8').read()
    if v1.startswith('ERROR'): c['v1-rejected'] += 1; continue
    if pr.startswith('ERROR'): c['pr-error'] += 1; continue
    if pr == v1: c['agree'] += 1
    else: c['diverge'] += 1; div.append(os.path.basename(f))
print(suf, dict(c))
open(os.path.join(d, '..', 'diverging' + suf + '.txt'), 'w').write('\n'.join(div))

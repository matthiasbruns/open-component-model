import sys, os, json, collections
d = sys.argv[1]
names = open(os.path.join(d, '..', 'diverging.txt')).read().split()
def html(s): return s.replace('&', '\\u0026').replace('<', '\\u003c').replace('>', '\\u003e')
def diffpaths(a, b, p=''):
    if type(a) != type(b): return [p + ' (type %s vs %s)' % (type(a).__name__, type(b).__name__)]
    if isinstance(a, dict):
        if set(a) != set(b): return [p + ' keys %s vs %s' % (sorted(a), sorted(b))]
        r = []
        for k in a: r += diffpaths(a[k], b[k], p + '/' + k)
        return r
    if isinstance(a, list):
        if len(a) != len(b): return [p + ' len %d vs %d' % (len(a), len(b))]
        r = []
        for i, (x, y) in enumerate(zip(a, b)): r += diffpaths(x, y, p + '[]')
        return r
    return [] if a == b else [p + ' value']
classes = collections.Counter(); ex = {}
for n in names:
    f = os.path.join(d, n)
    pr = open(f + '.pr', encoding='utf-8').read(); v1 = open(f + '.v1', encoding='utf-8').read()
    cls = set()
    if html(pr) == v1: cls.add('BYTES: html-escape only')
    else:
        a, b = json.loads(pr), json.loads(v1)
        if a == b:
            cls.add('BYTES: other (semantically equal)')
        else:
            for p in diffpaths(a, b): cls.add('TREE: ' + p[:160])
            # also html part
            if html(pr) != pr: cls.add('(+html chars present)')
    for c in cls:
        classes[c] += 1; ex.setdefault(c, n)
for c, k in classes.most_common(60): print(k, c, ex[c])

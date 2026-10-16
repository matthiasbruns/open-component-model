import re, subprocess, sys, os
src = open(sys.argv[1]).read()
consts = dict(re.findall(r'(\w+)\s*=\s*`([^`]*)`', src))
oracle = sys.argv[2]
fail = 0
for name in sorted(k for k in consts if k.startswith('input_')):
    key = name[len('input_'):]
    exp = consts.get('norm_' + key)
    p = os.path.join(sys.argv[3], key + '.yaml')
    open(p, 'w').write(consts[name])
    r = subprocess.run([sys.executable, '-I', oracle, p], capture_output=True, text=True, env=os.environ)
    if r.returncode:
        print(f'{key}: cc-utils ERROR {r.stderr.strip().splitlines()[-1]}'); continue
    got = r.stdout
    if got == exp:
        print(f'{key}: MATCH'); continue
    fail = 1
    i = 0
    while i < min(len(got), len(exp)) and got[i] == exp[i]: i += 1
    print(f'{key}: DIFF\n  cc-utils: ...{got[max(0,i-40):i+60]}\n  PR fixt : ...{exp[max(0,i-40):i+60]}')
sys.exit(fail)

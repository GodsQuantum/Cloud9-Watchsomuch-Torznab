#!/usr/bin/env python3
from pathlib import Path
import subprocess,re,xml.etree.ElementTree as ET
base=Path(__file__).resolve().parent.parent
tracked=subprocess.check_output(['git','-C',str(base),'ls-files','-z']).decode().split(chr(0))
private=[re.compile(r,re.I) for r in ['a'+'rezki','chou'+'gar','kech'+'idi','fe'+'lla',
 r'192[.]168[.]',r'home[.]arpa',r'@gmail[.]com','/srv/'+'lxc/',r'-----BEGIN PRIVATE KEY',
 r'ghp_[a-zA-Z0-9]{25,}',r'github_pat_[A-Za-z0-9_]{25,}']]
bad=[]
for name in tracked:
 if not name or name=='tests/privacy.py':continue
 file=base/name
 if not file.is_file() or file.stat().st_size>350000:continue
 if any(x.search(file.read_text(errors='replace')) for x in private):bad.append(name)
assert not bad,('PRIVATE_DATA_PUBLICATION_BLOCKED',bad)
ET.parse(base/'docs/logo.svg')
ET.parse(base/'docs/architecture.svg')
print('PUBLIC_WSM_SOURCE_PRIVACY_AND_DIAGRAMS_PASS')

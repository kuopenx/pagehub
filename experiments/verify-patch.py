"""Live MCP acceptance: all deployed page mutations use tools/call over HTTP."""
import datetime
import hashlib
import json
import urllib.request
from pathlib import Path

base = Path(__file__).resolve().parent
endpoint = 'http://127.0.0.1:8765/_mcp'
token = (Path.home() / '.pagehub/token').read_text().strip()
protocol = '2026-07-28'
request_id = 0

def rpc(method, params, notification=False):
    global request_id
    request_id += 1
    if not notification:
        params = dict(params, _meta={
            'io.modelcontextprotocol/protocolVersion': protocol,
            'io.modelcontextprotocol/clientInfo': {'name':'pagehub-live-patch-test', 'version':'1'},
            'io.modelcontextprotocol/clientCapabilities': {}})
    body = dict(jsonrpc='2.0', method=method, params=params)
    if not notification:
        body['id'] = request_id
    headers = {
        'Authorization': 'Bearer '+token, 'Content-Type': 'application/json',
        'Accept': 'application/json, text/event-stream', 'MCP-Protocol-Version': protocol,
        'MCP-Method': method}
    if method == 'tools/call':
        headers['MCP-Name'] = params['name']
    req = urllib.request.Request(endpoint, data=json.dumps(body).encode(), headers=headers)
    with urllib.request.urlopen(req, timeout=15) as response:
        raw = response.read()
    if notification:
        return None
    result = json.loads(raw)
    assert 'error' not in result, result.get('error')
    return result['result']

def call(name, args, fail=False):
    result = rpc('tools/call', dict(name=name, arguments=args))
    if fail:
        assert result.get('isError'), f'{name} unexpectedly succeeded'
        return result.get('content')
    assert not result.get('isError'), result.get('content')
    return result['structuredContent']

hello = rpc('server/discover', {})
assert protocol in hello['supportedVersions']
names = sorted(t['name'] for t in rpc('tools/list', {})['tools'])
assert names == sorted(['create_page','update_page','delete_page','list_pages','read_page','patch_page'])
listing = call('list_pages', dict(limit=0))
pages_before = {p['id']:p for p in listing['pages']}
pelicans = [p for p in listing['pages'] if p['id']=='c67b3c1d-7094-47fa-9b89-a141c4010fb3']
assert len(pelicans)==1
id = pelicans[0]['id']
original = call('read_page', dict(id=id))
source = original['content']
assert 'id="wind"' not in source, 'Experiment already applied; do not apply twice.'
(base / 'pelican-before.html').write_text(source)
others = {pid:call('read_page', dict(id=pid))['content'] for pid in pages_before if pid!=id}
part = call('read_page', dict(id=id, start_line=85, end_line=100))
assert part['content']==''.join(source.splitlines(keepends=True)[84:100])
assert original['page']['revision']==pelicans[0]['revision']
revision = original['page']['revision']
edits = json.loads((base / 'pelican-edits.json').read_text())
expected = source
for edit in edits:
    assert expected.count(edit['old_text'])==1
    expected = expected.replace(edit['old_text'], edit['new_text'], 1)
failed = call('patch_page', dict(id=id, expected_revision=revision, edits=[edits[0], dict(old_text='__pagehub_missing_anchor_6d0ea1__', new_text='x')]), fail=True)
assert call('read_page', dict(id=id))==original, 'Failed batch changed page'
ambiguous = call('patch_page', dict(id=id, expected_revision=revision, edits=[dict(old_text='<circle', new_text='<ellipse')]), fail=True)
assert call('read_page', dict(id=id))==original, 'Ambiguous edit changed page'
patched = call('patch_page', dict(id=id, expected_revision=revision, edits=edits))
assert patched['applied_edits']==len(edits)
assert patched['page']['revision']==revision+1
assert patched['page']['created_at']==original['page']['created_at']
assert patched['page']['url']==original['page']['url']
after = call('read_page', dict(id=id))
assert after['content']==expected
stale = call('patch_page', dict(id=id, expected_revision=revision, edits=[dict(old_text='今天，慢慢骑。', new_text='stale')]), fail=True)
assert call('read_page', dict(id=id))==after, 'Stale edit changed page'
for pid, html in others.items():
    assert call('read_page', dict(id=pid))['content']==html, 'Unrelated page modified'
listing_after = call('list_pages', dict(limit=0))
assert listing_after['total']==listing['total']
for pid, old in pages_before.items():
    if pid!=id:
        assert next(p for p in listing_after['pages'] if p['id']==pid)==old
for url in [after['page']['url'], 'http://192.168.31.211:8765'+after['page']['path']]:
    # Public LAN requests intentionally carry NO management token.
    with urllib.request.urlopen(url, timeout=10) as response:
        assert response.status==200 and response.read().decode()==expected
with urllib.request.urlopen('http://192.168.31.211:8765/', timeout=10) as response:
    assert id in response.read().decode()
(base / 'pelican-after.html').write_text(after['content'])
report = dict(verified_at=datetime.datetime.now(datetime.timezone.utc).isoformat(), protocol=protocol,
    tools=names, page=after['page'], applied_edits=len(edits), checks=['actual MCP discover/list/read/patch', 'exact line-range read', 'failed batch leaves content and metadata unchanged', 'ambiguous text rejected', 'stale revision rejected', 'URL and creation time preserved', 'unrelated pages unchanged', 'localhost and LAN serve patched HTML', 'LAN dashboard accessible'],
    expected_errors=dict(failed_batch=failed, ambiguous=ambiguous, stale=stale), sha256=hashlib.sha256(expected.encode()).hexdigest())
(base / 'verification-patch.json').write_text(json.dumps(report, ensure_ascii=False, indent=2)+'\n')
print(json.dumps(report, ensure_ascii=False, indent=2))

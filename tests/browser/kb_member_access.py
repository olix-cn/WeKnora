"""Opt-in compatibility WebKit loop for TestKBMemberRoutesRealRepository.

The new system-center UI is adapted to the legacy real member-policy transport;
this checks the shared editor, not authorization of the new platform routes. Other
page dependencies are synthetic fixtures; this is not a full server/login E2E.
Requires existing Playwright/WebKit and frontend/dist, installs nothing.
"""
import http.server
import json
import pathlib
import sys
import threading
from urllib.parse import urlparse

from playwright.sync_api import sync_playwright

BACKEND = sys.argv[1]
ROOT = pathlib.Path(__file__).resolve().parents[2] / 'frontend' / 'dist'

class Handler(http.server.SimpleHTTPRequestHandler):
    def __init__(self, *args, **kwargs):
        super().__init__(*args, directory=str(ROOT), **kwargs)

    def do_GET(self):
        if self.path.startswith('/platform/'):
            self.path = '/index.html'
        super().do_GET()

    def log_message(self, *args):
        pass

server = http.server.ThreadingHTTPServer(('127.0.0.1', 0), Handler)
threading.Thread(target=server.serve_forever, daemon=True).start()

try:
    with sync_playwright() as p:
        browser = p.webkit.launch()
        page = browser.new_page(viewport={'width': 1440, 'height': 1100})
        page.set_default_timeout(15000)
        errors, saves = [], []
        page.on('pageerror', lambda e: errors.append(str(e)))

        def api(route):
            path = urlparse(route.request.url).path
            if '/member-access' in path or '/knowledge-base-transfers' in path or '/knowledge-base-permissions/kb' in path:
                backend_path = path.replace('/system/admin/knowledge-base-permissions/kb/candidates', '/knowledge-bases/kb/member-access/candidates').replace('/system/admin/knowledge-base-permissions/kb', '/knowledge-bases/kb/member-access')
                response = route.fetch(url=BACKEND + backend_path + ('?' + urlparse(route.request.url).query if urlparse(route.request.url).query else ''), headers={**route.request.headers, 'X-Test-User': 'owner'})
                if route.request.method == 'PUT':
                    saves.append((response.status, route.request.post_data_json))
                route.fulfill(response=response)
                return
            data = {'success': True, 'data': [], 'total': 0}
            if path.endswith('/system/admin/knowledge-base-permissions'):
                data = {'data': [{'id': 'kb', 'name': 'Synthetic KB', 'tenant_id': 7, 'tenant_name': 'Synthetic workspace', 'creator_id': 'owner', 'mode': 'restricted', 'version': 1, 'member_count': 1}], 'total': 1}
            elif path.endswith('/auth/me'):
                data['data'] = {'user': {'id': 'owner', 'username': 'Synthetic owner', 'is_system_admin': True},
                                'tenant': {'id': 7, 'name': 'Synthetic workspace'},
                                'memberships': [{'tenant_id': 7, 'role': 'owner'}]}
            elif path.endswith('/knowledge-bases/kb'):
                data['data'] = {'id': 'kb', 'name': 'Synthetic KB', 'tenant_id': 7, 'creator_id': 'owner',
                                'type': 'document', 'my_permission': 'admin', 'chunking_config': {}, 'embedding_model_id': '',
                                'knowledge_count': 0, 'chunk_count': 0, 'storage_size': 0}
            route.fulfill(status=200, content_type='application/json', body=json.dumps(data))

        page.route('**/api/**', api)
        page.add_init_script("""localStorage.setItem('locale','zh-CN');
          localStorage.setItem('weknora_token','synthetic-test-only');
          localStorage.setItem('weknora:new-user-guide-done:v1','1');
          localStorage.setItem('weknora:contextual-guide-kb-detail:v1','1');
          localStorage.setItem('weknora:assistant-entry-guide-done:v0.9.0','1');""")
        page.goto(f'http://127.0.0.1:{server.server_port}/platform/settings?section=knowledge-base-permissions')
        page.get_by_role('button', name='配置权限', exact=True).click()
        panel = page.locator('.kb-member-access')
        panel.wait_for()
        panel.get_by_text('delegate', exact=True).wait_for()
        panel.get_by_role('button', name='移除', exact=True).click()
        panel.locator('.t-checkbox').click()
        panel.get_by_role('button', name='保存权限', exact=True).click()
        panel.wait_for(state='hidden')
        assert len(saves) == 1 and saves[0][0] == 200, saves
        assert saves[0][1] == {'mode': 'restricted', 'expected_version': 1, 'members': []}, saves
        page.get_by_role('button', name='配置权限', exact=True).click()
        panel.get_by_text('当前仅空间所有者和管理员可以访问。', exact=True).wait_for()
        assert not errors, errors
        print(json.dumps({'real_policy_http_save_reload': True, 'runtime_errors': errors}))
        browser.close()
finally:
    server.shutdown()

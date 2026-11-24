"""Synthetic-API WebKit UI smoke for the system policy center, not backend E2E."""
import http.server
import json
import pathlib
import threading
from urllib.parse import urlparse
from playwright.sync_api import sync_playwright

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
policy = {'mode': 'inherit', 'version': 0, 'members': [], 'member_details': []}
saves = []
def api(route):
    path = urlparse(route.request.url).path
    data = {'success': True, 'data': [], 'total': 0}
    if path.endswith('/auth/me'):
        user = {'id': 'admin', 'username': 'Synthetic admin', 'is_system_admin': True, 'tenant_id': 7}
        tenant = {'id': 7, 'name': 'Home workspace'}
        memberships = [{'tenant_id': 7, 'role': 'owner'}]
        data = {'success': True, 'user': user, 'tenant': tenant, 'active_tenant': tenant,
                'memberships': memberships, 'data': {'user': user, 'tenant': tenant, 'memberships': memberships}}
    elif path.endswith('/system/admin/knowledge-base-permissions'):
        data = {'data': [{'id': 'kb', 'name': 'Synthetic KB', 'tenant_id': 8, 'tenant_name': 'Other workspace',
                          'creator_id': 'creator', 'mode': policy['mode'], 'version': policy['version'],
                          'member_count': len(policy['members'])}], 'total': 1}
    elif path.endswith('/knowledge-base-permissions/kb/candidates'):
        data = {'data': [{'user_id': 'reader', 'username': 'Synthetic reader', 'role': 'contributor'}], 'total': 1}
    elif path.endswith('/knowledge-base-permissions/kb'):
        if route.request.method == 'PUT':
            body = route.request.post_data_json
            assert body == {'mode': 'restricted', 'expected_version': 0, 'members': [{'user_id': 'reader', 'permission': 'viewer'}]}, body
            saves.append(body)
            policy.update(mode=body['mode'], version=1, members=body['members'])
            policy['member_details'] = [{'user_id': 'reader', 'username': 'Synthetic reader', 'role': 'contributor'}]
        data = {'success': True, 'data': policy}
    elif path.endswith('/system/capabilities'):
        data = {'edition': 'standard', 'capabilities': {'organizations': {'supported': True}}}
    route.fulfill(status=200, content_type='application/json', body=json.dumps(data))
try:
    with sync_playwright() as p:
        browser = p.webkit.launch()
        page = browser.new_page(viewport={'width': 1440, 'height': 1000})
        errors = []
        page.on('pageerror', lambda e: errors.append(str(e)))
        page.add_init_script("""localStorage.setItem('locale','zh-CN');localStorage.setItem('weknora_token','synthetic');
          localStorage.setItem('weknora:new-user-guide-done:v1','1');localStorage.setItem('weknora:assistant-entry-guide-done:v0.9.0','1');""")
        page.route('**/api/**', api)
        page.goto(f'http://127.0.0.1:{server.server_port}/platform/settings?section=knowledge-base-permissions')
        page.locator('.kb-permissions-center').get_by_text('Synthetic KB', exact=True).wait_for()
        assert page.locator('.nav-item').filter(has_text='知识库权限').count() == 1
        page.get_by_role('button', name='配置权限', exact=True).click()
        panel = page.locator('.kb-member-access')
        panel.get_by_role('button', name='指定成员', exact=False).click()
        # Switching from inherit explicitly seeds the creator; remove it before granting reader.
        panel.get_by_role('button', name='移除', exact=True).click()
        panel.get_by_role('button', name='搜索', exact=True).click()
        panel.locator('.candidates').get_by_text('Synthetic reader', exact=True).wait_for()
        panel.get_by_role('button', name='添加', exact=True).click()
        panel.locator('.t-checkbox').click()
        panel.get_by_role('button', name='保存权限', exact=True).click()
        panel.wait_for(state='hidden')
        assert len(saves) == 1
        page.get_by_role('button', name='配置权限', exact=True).click()
        panel.locator('.grants').get_by_text('Synthetic reader', exact=True).wait_for()
        assert not errors, errors
        print(json.dumps({'global_policy_center_save_reload': True, 'cross_workspace_metadata': True,
                          'synthetic_api_only': True, 'runtime_errors': errors}))
        browser.close()
finally:
    server.shutdown()

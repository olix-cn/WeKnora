"""Opt-in WebKit UI smoke using synthetic API fixtures, not backend E2E.
Run after building frontend/dist with existing Playwright/WebKit; installs nothing.
"""
import http.server,json,pathlib,threading
from urllib.parse import urlparse
from playwright.sync_api import sync_playwright
ROOT=pathlib.Path(__file__).resolve().parents[2] / 'frontend' / 'dist'
class Handler(http.server.SimpleHTTPRequestHandler):
 def __init__(self,*a,**kw):super().__init__(*a,directory=str(ROOT),**kw)
 def do_GET(self):
  if self.path.startswith('/platform/'):self.path='/index.html'
  super().do_GET()
 def log_message(self,*a):pass
server=http.server.ThreadingHTTPServer(('127.0.0.1',0),Handler)
threading.Thread(target=server.serve_forever,daemon=True).start()
departments=[]
def api(route, *, role='owner', system_admin=True, toolbox=False, switcher=False, department=''):
 path=urlparse(route.request.url).path
 data={'success':True,'data':[],'total':0}
 if path.endswith('/auth/me'):
  data={'success':True,'user':{'id':'admin','username':'Synthetic admin','is_system_admin':system_admin,'tenant_id':7},'tenant':{'id':7,'name':'Synthetic workspace'},'active_tenant':{'id':7,'name':'Synthetic workspace'},'memberships':[{'tenant_id':7,'role':role}]}
  if not system_admin:
   data['user']['username']='test1'
   data['memberships'].append({'tenant_id':8,'tenant_name':'Assigned workspace','role':'viewer'})
  data['data']={'user':data['user'],'tenant':data['tenant'],'memberships':data['memberships'],
                 'department_name':department,'capabilities':{'can_create_tenant':True}}
 elif path.endswith('/system/admin/directory'):data={'directory':{'name':'Synthetic Company','version':1},'departments':departments}
 elif path.endswith('/system/admin/directory/members'):data={'users':[{'id':'user','username':'Sample','email':'','is_active':True,'is_system_admin':False,'department_name':'','version':0,'created_at':'2026-11-19T00:00:00Z'}],'total':1,'offset':0,'limit':20,'has_more':False}
 elif path.endswith('/system/admin/directory/departments') and route.request.method=='POST':
  payload=route.request.post_data_json
  assert payload['name']=='Synthetic Engineering'
  assert 'code' not in payload
  item={'id':'dept','name':payload['name'],'code':'DEP-SYNTHETIC','sort_order':0,'parent_id':None,'leader_id':None,'version':1,'member_count':0};departments.append(item);data=item
 elif path.endswith('/system/capabilities'):
  data={'code':0,'msg':'success','data':{'edition':'standard','capabilities':{'organizations':{'supported':True},'navigation.toolbox':{'supported':toolbox},'navigation.workspace_switcher':{'supported':switcher}}}}
 elif path.endswith('/system/admin/settings'):data=[]
 route.fulfill(status=200,content_type='application/json',body=json.dumps(data))
try:
 with sync_playwright() as p:
  browser=p.webkit.launch();page=browser.new_page(viewport={'width':1600,'height':1100});errors=[];page.on('console',lambda m: print('console',m.type,m.text,flush=True) if m.type in ['error','warning'] else None);page.on('pageerror',lambda e:errors.append(str(e)))
  page.add_init_script("""localStorage.setItem('locale','zh-CN');localStorage.setItem('weknora_token','synthetic');localStorage.setItem('weknora:new-user-guide-done:v1','1');localStorage.setItem('weknora:assistant-entry-guide-done:v0.9.0','1');""")
  page.route('**/api/**', lambda route: api(route))
  page.goto(f'http://127.0.0.1:{server.server_port}/platform/settings?section=organization-management')
  page.locator('.corporate-directory').wait_for(timeout=20000)
  assert page.locator('[data-guide=nav-toolbox]').is_visible()
  assert page.locator('.nav-item').filter(has_text='组织管理').count()==1
  page.locator('.corporate-directory').get_by_text('Sample',exact=True).first.wait_for()
  bounds=page.locator('.user-management__table').bounding_box()
  assert bounds and bounds['width'] > 850, bounds
  directory = page.locator('.corporate-directory')
  panel = directory.locator('.department-panel')
  assert directory.get_by_role('button', name='收起部门', exact=True).count() == 0
  assert directory.locator('.directory-members').get_by_text('全部成员', exact=True).count() == 0
  panel.get_by_role('button', name='新增部门', exact=True).click()
  dialog=page.locator('.t-dialog:visible').filter(has_text='部门名称')
  dialog.locator('input').first.fill('Synthetic Engineering')
  dialog.get_by_role('button',name='确认',exact=True).click()
  department = panel.get_by_role('button', name='Synthetic Engineering', exact=False)
  department.click()
  assert department.get_attribute('aria-pressed') == 'true'
  assert directory.locator('.directory-members').get_by_text('Synthetic Engineering', exact=True).count() == 0
  actions = panel.locator('.department-actions')
  actions.get_by_role('button', name='新增子部门', exact=True).wait_for()
  actions.get_by_role('button', name='删除', exact=True).wait_for()
  actions.get_by_role('button', name='编辑', exact=True).click()
  editing = page.locator('.t-dialog:visible').filter(has_text='编辑部门')
  assert editing.locator('input').first.input_value() == 'Synthetic Engineering'
  assert not errors,errors
  assert len(departments)==1
  for toolbox, switcher, department in [(False, False, '技术部'), (True, False, ''), (False, True, '研发部')]:
   member = browser.new_page(viewport={'width':1600,'height':1100})
   member.on('pageerror', lambda e: errors.append(str(e)))
   member.add_init_script("localStorage.setItem('locale','zh-CN');localStorage.setItem('weknora_token','synthetic');")
   member.route('**/api/**', lambda route: api(route, role='viewer', system_admin=False, toolbox=toolbox, switcher=switcher, department=department))
   member.goto(f'http://127.0.0.1:{server.server_port}/platform/settings?section=models')
   member.wait_for_url('**/platform/settings?section=general')
   assert member.locator('.nav-item .nav-label').all_text_contents() == ['常规设置', '用户信息']
   assert member.locator('[data-guide=nav-toolbox]').is_visible() == toolbox, toolbox
   assert member.locator('[data-guide=nav-agents]').count() == 0
   assert member.locator('[data-guide=nav-knowledge-bases]').is_visible()
   member.locator('.settings-modal [aria-label="关闭"]').first.click()
   member.wait_for_url('**/platform/knowledge-bases')
   assert member.locator('[data-guide=kb-list-create]').count() == 0
   account = member.locator('[data-guide=user-menu]')
   assert account.locator('.user-name').inner_text() == f'test1 · {department or "未分配部门"}'
   account.click()
   dropdown = member.locator('.user-dropdown')
   assert dropdown.get_by_text('个人设置', exact=True).is_visible()
   assert dropdown.get_by_text('全部设置', exact=True).count() == 0
   assert dropdown.get_by_text('空间设置', exact=True).count() == 0
   switcher_panel = dropdown.locator('.dropdown-tenant-panel')
   assert switcher_panel.is_visible() == switcher
   if switcher:
    switcher_panel.hover()
    member.locator('.tenant-submenu-floating').get_by_text('Assigned workspace', exact=True).wait_for()
    assert member.locator('.tenant-submenu-create').count() == 0
   else:
    assert member.locator('.tenant-submenu-floating').count() == 0
   member.close()
  assert not errors, errors
  print(json.dumps({'organization_actions_on_department_side':True,'navigation_for_admin_and_member':True,'mock_page_dependencies':True,'runtime_errors':errors}))
  browser.close()
finally:server.shutdown()

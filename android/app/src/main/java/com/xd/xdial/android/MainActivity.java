package com.xd.xdial.android;

import android.app.*;
import android.os.*;
import android.content.*;
import android.net.VpnService;
import android.view.*;
import android.widget.*;
import libbox.Libbox;

public final class MainActivity extends Activity {
 private TextView status;
 private DialView dial;
 private Switch automatic;
 private Button scenarios;
 private boolean refreshingSelection;
 private String pendingSetupLine="";
 private boolean lastActive;
 private long lastSelectionRevision;
 private String lastConnectionState="";
 private final Handler handler = new Handler();
 private final Runnable refresh = new Runnable() { public void run() { status.setText(TunnelService.state+"\n"+TunnelService.surveyResult+"\n"+TunnelService.probeResult); dial.setActive(TunnelService.active);if(lastActive!=TunnelService.active||lastSelectionRevision!=TunnelService.selectionRevision||!lastConnectionState.equals(TunnelService.state)){lastSelectionRevision=TunnelService.selectionRevision;lastActive=TunnelService.active;lastConnectionState=TunnelService.state;refreshSelection();}handler.postDelayed(this, 500); } };
 @Override public void onCreate(Bundle saved) {
  super.onCreate(saved);
  ScrollView scroll=new ScrollView(this);scroll.setFillViewport(true);
  LinearLayout layout = new LinearLayout(this); layout.setOrientation(1);layout.setPadding(dp(24),dp(16),dp(24),dp(24));scroll.addView(layout);
  scroll.setOnApplyWindowInsetsListener((view,insets)->{if(Build.VERSION.SDK_INT>=30){android.graphics.Insets bars=insets.getInsets(WindowInsets.Type.systemBars());view.setPadding(bars.left,bars.top,bars.right,bars.bottom);}else{view.setPadding(insets.getSystemWindowInsetLeft(),insets.getSystemWindowInsetTop(),insets.getSystemWindowInsetRight(),insets.getSystemWindowInsetBottom());}return insets;});
  TextView title = new TextView(this); title.setText("XDial"); title.setTextSize(28);title.setGravity(Gravity.CENTER); layout.addView(title);
  status = new TextView(this); status.setTextSize(16);status.setGravity(Gravity.CENTER); layout.addView(status);
  dial = new DialView(this);
  layout.addView(dial, new LinearLayout.LayoutParams(-1, dp(300)));
  dial.setOnClickListener(v -> {
   if (TunnelService.active) { startService(new Intent(this,TunnelService.class).setAction("stop")); return; }
   Intent consent = VpnService.prepare(this);
   if (consent != null) startActivityForResult(consent, 1); else connect();
  });
  scenarios = new Button(this);scenarios.setAllCaps(false); scenarios.setText("选择场景"); layout.addView(scenarios);
  scenarios.setOnClickListener(v -> chooseScenario());
  automatic = new Switch(this); automatic.setText("连接前按 Wi-Fi 或地区自动选择");
  automatic.setChecked(true); layout.addView(automatic);
  automatic.setOnCheckedChangeListener((button,enabled)->{
   if(refreshingSelection)return;
   try {MobileSelection.get(this).automatic(enabled);
    if(enabled && checkSelfPermission(android.Manifest.permission.ACCESS_FINE_LOCATION)!=android.content.pm.PackageManager.PERMISSION_GRANTED)
     requestPermissions(new String[]{android.Manifest.permission.ACCESS_FINE_LOCATION},2);
   } catch(Exception e){Toast.makeText(this,"请先导入配置，连接期间不能改变选择",Toast.LENGTH_SHORT).show();refreshSelection();}
  });
  Button editor=new Button(this);editor.setText("编辑场景");layout.addView(editor);editor.setOnClickListener(v->startActivity(new Intent(this,ScenariosActivity.class)));
  Button conditions=new Button(this);conditions.setText("自动场景条件");layout.addView(conditions);
  conditions.setOnClickListener(v->editConditions());
  Button login=new Button(this);login.setText("添加 AnyConnect 账号与场景");layout.addView(login);login.setOnClickListener(v->addVPN());
  Button tailscale=new Button(this);tailscale.setText("Tailscale 账号与出口节点");layout.addView(tailscale);tailscale.setOnClickListener(v->tailscale());
  Button config = new Button(this); config.setText("配置来源"); layout.addView(config);
  config.setOnClickListener(v -> startActivity(new Intent(this,ProfileSourcesActivity.class)));
  Button rules=new Button(this);rules.setText("规则与离线副本");layout.addView(rules);
  rules.setOnClickListener(v->startActivity(new Intent(this,OfflineRulesActivity.class)));
  Button details=new Button(this);details.setText("连接诊断");layout.addView(details);
  details.setOnClickListener(v->new AlertDialog.Builder(this).setTitle("连接诊断").setItems(new String[]{"记录运行计数","验证 DNS 与 HTTPS","验证安全存储"},(d,index)->{if(TunnelService.active)startService(new Intent(this,TunnelService.class).setAction(index==0?"diagnose":index==1?"probe":"identity-test"));}).show());
  TextView notice = new TextView(this); notice.setText("开发验收版：支持手动场景、连接前 Wi-Fi 优先与地区判断。手动选择关闭自动选择。AnyConnect 支持账号密码，Tailscale 支持浏览器登录与出口选择；连接中可选择场景，准备失败保留原连接。真实企业网络联调仍待完成。"); layout.addView(notice);
  setContentView(scroll);
 }
 private int dp(int value){return Math.round(value*getResources().getDisplayMetrics().density);}
 private void editConditions(){
  if(TunnelService.active){Toast.makeText(this,"请先断开再编辑条件",Toast.LENGTH_SHORT).show();return;}
  try{
   String original=new ProfileStore(this).load();org.json.JSONObject profile=new org.json.JSONObject(original);
   String id=profile.getString("active_scenario_id");
   org.json.JSONObject value=new org.json.JSONObject(Libbox.scenarioConditions(original,id));
   LinearLayout fields=new LinearLayout(this);fields.setOrientation(1);fields.setPadding(dp(24),0,dp(24),0);
   TextView help=new TextView(this);help.setText("编辑当前场景的条件。Wi-Fi 名称优先；没有匹配时，才查询网络出口地区。地区代码用逗号分开，例如 CN, JP；* 表示其它地区。查询失败保留原场景，清空字段可移除条件。");fields.addView(help);
   EditText ssids=new EditText(this);ssids.setHint("Wi-Fi 名称，每行一个");ssids.setInputType(android.text.InputType.TYPE_CLASS_TEXT|android.text.InputType.TYPE_TEXT_FLAG_MULTI_LINE|android.text.InputType.TYPE_TEXT_FLAG_NO_SUGGESTIONS);ssids.setText(join(value.getJSONArray("ssids"),"\n"));fields.addView(ssids);
   EditText countries=new EditText(this);countries.setHint("地区代码，例如 CN, JP 或 *");countries.setInputType(android.text.InputType.TYPE_CLASS_TEXT|android.text.InputType.TYPE_TEXT_FLAG_NO_SUGGESTIONS);countries.setText(join(value.getJSONArray("countries"),", "));fields.addView(countries);
   ssids.setSaveEnabled(false);countries.setSaveEnabled(false);ssids.setImportantForAutofill(View.IMPORTANT_FOR_AUTOFILL_NO);countries.setImportantForAutofill(View.IMPORTANT_FOR_AUTOFILL_NO);
   TextView error=new TextView(this);fields.addView(error);
   AlertDialog dialog=new AlertDialog.Builder(this).setTitle("自动场景条件").setView(fields).setNegativeButton("取消",null).setPositiveButton("保存",null).create();
   dialog.setOnShowListener(d->{dialog.getWindow().addFlags(WindowManager.LayoutParams.FLAG_SECURE);dialog.getButton(AlertDialog.BUTTON_POSITIVE).setOnClickListener(v->{
    if(TunnelService.active){error.setText("连接期间不能编辑，请先断开。");return;}
    try{
     String updated=Libbox.updateScenarioConditions(original,id,ssids.getText().toString(),countries.getText().toString());
     new ProfileStore(this).save(updated);MobileSelection.invalidate();refreshSelection();dialog.dismiss();
     Toast.makeText(this,"条件已保存；启用自动选择后，下次连接使用",Toast.LENGTH_LONG).show();
    }catch(Exception failure){error.setText("请检查地区代码、Wi-Fi 名称长度，以及是否与其它场景重复；配置变化时请重新打开。");}
   });});dialog.show();
  }catch(Exception failure){Toast.makeText(this,"无法读取场景条件",Toast.LENGTH_LONG).show();}
 }
 private String join(org.json.JSONArray values,String separator)throws Exception{
  StringBuilder result=new StringBuilder();for(int i=0;i<values.length();i++){if(i>0)result.append(separator);result.append(values.getString(i));}return result.toString();
 }
 private void connect() { startForegroundService(new Intent(this,TunnelService.class)); }
 @Override protected void onActivityResult(int request,int result,Intent data) { super.onActivityResult(request,result,data); if(request==1 && result==RESULT_OK) connect();if(request==3 && result==RESULT_OK)startTailscale(); }
 private void chooseScenario() {
  if(TunnelService.settingUp){Toast.makeText(this,"请先结束账号设置",Toast.LENGTH_SHORT).show();return;}
  try {
   MobileSelection selection=MobileSelection.get(this);
   org.json.JSONObject state=selection.snapshot();org.json.JSONArray choices=state.getJSONArray("choices");
   String[] names=new String[choices.length()];String[] ids=new String[choices.length()];int selected=-1;
   for(int i=0;i<choices.length();i++){org.json.JSONObject choice=choices.getJSONObject(i);names[i]=choice.getString("name");ids[i]=choice.getString("id");if(ids[i].equals(state.getString("selected")))selected=i;}
   new AlertDialog.Builder(this).setTitle("场景").setSingleChoiceItems(names,selected,(d,index)->{
    try {
     if(TunnelService.active)startService(new Intent(this,TunnelService.class).setAction("switch-scenario").putExtra("scenario",ids[index]));
     else{selection.select(ids[index]);refreshSelection();Toast.makeText(this,"场景已选择",Toast.LENGTH_SHORT).show();}
     d.dismiss();
    }
    catch(Exception e){Toast.makeText(this,"无法保存场景，原配置保留",Toast.LENGTH_LONG).show();}
   }).setNegativeButton("取消",null).show();
  } catch(Exception e){Toast.makeText(this,"请先导入有效配置",Toast.LENGTH_LONG).show();}
 }
 private void addVPN(){
  if(TunnelService.active){Toast.makeText(this,"请先断开再添加账号",Toast.LENGTH_SHORT).show();return;}
  LinearLayout fields=new LinearLayout(this);fields.setOrientation(LinearLayout.VERTICAL);fields.setPadding(dp(24),0,dp(24),0);
  EditText name=new EditText(this);name.setHint("场景名称");fields.addView(name);
  EditText server=new EditText(this);server.setHint("服务器，例如 vpn.example.com");server.setInputType(android.text.InputType.TYPE_CLASS_TEXT|android.text.InputType.TYPE_TEXT_VARIATION_URI);fields.addView(server);
  EditText user=new EditText(this);user.setHint("账号");user.setSingleLine(true);user.setInputType(android.text.InputType.TYPE_CLASS_TEXT|android.text.InputType.TYPE_TEXT_FLAG_NO_SUGGESTIONS);fields.addView(user);
  EditText password=new EditText(this);password.setHint("密码");password.setInputType(android.text.InputType.TYPE_CLASS_TEXT|android.text.InputType.TYPE_TEXT_VARIATION_PASSWORD);password.setImportantForAutofill(View.IMPORTANT_FOR_AUTOFILL_NO);fields.addView(password);
  TextView hint=new TextView(this);hint.setText("保存后拨动拨盘连接。服务器证书会正常校验；账号密码仅保存在本机加密存储中。");fields.addView(hint);
  AlertDialog dialog=new AlertDialog.Builder(this).setTitle("AnyConnect").setView(fields).setNegativeButton("取消",null).setPositiveButton("保存",null).create();
  dialog.setOnShowListener(d->{dialog.getWindow().addFlags(WindowManager.LayoutParams.FLAG_SECURE);dialog.getButton(AlertDialog.BUTTON_POSITIVE).setOnClickListener(v->{
   try{
    ProfileStore store=new ProfileStore(this);String existing;
    try{existing=store.load();}catch(java.io.FileNotFoundException missing){existing="";}
    String updated=Libbox.addAnyConnectScenario(existing,name.getText().toString(),server.getText().toString(),user.getText().toString(),password.getText().toString());
    store.save(updated);MobileSelection.invalidate();refreshSelection();password.setText("");dialog.dismiss();
    Toast.makeText(this,"场景已保存，拨动拨盘开始连接",Toast.LENGTH_LONG).show();
   }catch(Exception failure){Toast.makeText(this,"请检查必填项、服务器地址和本机存储",Toast.LENGTH_LONG).show();}
  });});
  dialog.setOnDismissListener(d->password.setText(""));dialog.show();
 }
 private void tailscale(){
  if(TunnelService.settingUp){showTailscale();return;}
  if(TunnelService.active){Toast.makeText(this,"请先断开再设置 Tailscale",Toast.LENGTH_SHORT).show();return;}
  new AlertDialog.Builder(this).setTitle("Tailscale").setMessage("准备登录并发现可用出口节点。此步骤不会接管设备流量；身份密钥加密保存在本机。")
   .setNegativeButton("取消",null).setPositiveButton("开始设置",(d,w)->{
    try{
     pendingSetupLine=new ProfileStore(this).prepareTailscale();MobileSelection.invalidate();refreshSelection();
     Intent consent=VpnService.prepare(this);if(consent!=null)startActivityForResult(consent,3);else startTailscale();
    }catch(Exception error){Toast.makeText(this,"无法准备 Tailscale，请检查配置与本机存储",Toast.LENGTH_LONG).show();}
   }).show();
 }
 private void startTailscale(){
  if(pendingSetupLine.isEmpty())return;
  startForegroundService(new Intent(this,TunnelService.class).setAction("tailscale-setup").putExtra("line",pendingSetupLine));
  handler.postDelayed(()->{if(!isFinishing())showTailscale();},500);
 }
 private void showTailscale(){
  LinearLayout content=new LinearLayout(this);content.setOrientation(1);content.setPadding(dp(24),0,dp(24),0);
  TextView info=new TextView(this);content.addView(info);
  Button login=new Button(this);login.setText("获取登录链接");content.addView(login);
  Button browser=new Button(this);browser.setText("在浏览器中登录");content.addView(browser);
  Button nodes=new Button(this);nodes.setText("选择出口节点");content.addView(nodes);
  Button logout=new Button(this);logout.setText("退出 Tailscale 登录");content.addView(logout);
  logout.setOnClickListener(v->new AlertDialog.Builder(this).setMessage("退出本机 Tailscale 登录？已保存的场景会保留，下次连接需要重新登录。")
   .setNegativeButton("取消",null).setPositiveButton("退出登录",(d,w)->startService(new Intent(this,TunnelService.class).setAction("tailscale-logout"))).show());
  final String[] authURL={""};
  login.setOnClickListener(v->startService(new Intent(this,TunnelService.class).setAction("tailscale-login")));
  browser.setOnClickListener(v->{
   if(!Libbox.validateTailscaleAuthURL(authURL[0]))return;
   try{startActivity(new Intent(Intent.ACTION_VIEW,android.net.Uri.parse(authURL[0])));}catch(Exception error){Toast.makeText(this,"没有可用浏览器，请安装浏览器后重试",Toast.LENGTH_LONG).show();}
  });
  nodes.setOnClickListener(v->{
   try{
    org.json.JSONArray all=new org.json.JSONObject(TunnelService.tailscaleStatus).getJSONArray("exit_nodes");
    java.util.ArrayList<String> names=new java.util.ArrayList<>(),ids=new java.util.ArrayList<>();
    for(int i=0;i<all.length();i++){org.json.JSONObject node=all.getJSONObject(i);if(node.optBoolean("online")){names.add(node.getString("name"));ids.add(node.getString("id"));}}
    if(ids.isEmpty()){Toast.makeText(this,"没有在线出口，请在 Tailscale 管理端启用出口节点",Toast.LENGTH_LONG).show();return;}
    new AlertDialog.Builder(this).setTitle("在线出口节点").setItems(names.toArray(new String[0]),(d,index)->startService(new Intent(this,TunnelService.class).setAction("tailscale-select").putExtra("node",ids.get(index)))).setNegativeButton("取消",null).show();
   }catch(Exception error){Toast.makeText(this,"节点列表暂不可用，请稍后重试",Toast.LENGTH_LONG).show();}
  });
  AlertDialog dialog=new AlertDialog.Builder(this).setTitle("Tailscale 账号").setView(content).setNegativeButton("收起",null)
   .setNeutralButton("结束设置",(d,w)->startService(new Intent(this,TunnelService.class).setAction("stop"))).create();
  Runnable update=new Runnable(){public void run(){
   if(!dialog.isShowing())return;
   String backend="";authURL[0]="";
   try{org.json.JSONObject status=new org.json.JSONObject(TunnelService.tailscaleStatus);backend=status.optString("backend_state");authURL[0]=status.optString("auth_url");}catch(Exception ignored){}
   String label=backend.equals("Running")?"已登录，可选择出口节点":backend.equals("NeedsMachineAuth")?"请在 Tailscale 管理端批准此设备":backend.equals("NeedsLogin")?"等待浏览器登录":"正在准备账号连接";
   info.setText(TunnelService.settingUp?label+"\n"+TunnelService.setupMessage:TunnelService.state);
   login.setEnabled(TunnelService.settingUp&&!backend.equals("Running"));browser.setEnabled(TunnelService.settingUp&&Libbox.validateTailscaleAuthURL(authURL[0]));nodes.setEnabled(TunnelService.settingUp&&backend.equals("Running"));logout.setEnabled(TunnelService.settingUp&&backend.equals("Running"));
   handler.postDelayed(this,500);
  }};
  dialog.setOnShowListener(d->{dialog.getWindow().addFlags(WindowManager.LayoutParams.FLAG_SECURE);handler.post(update);});
  dialog.setOnDismissListener(d->{handler.removeCallbacks(update);authURL[0]="";refreshSelection();});dialog.show();
 }
 private void refreshSelection() {
  refreshingSelection=true;
  try {
   org.json.JSONObject state=MobileSelection.get(this).snapshot();automatic.setEnabled(true);automatic.setChecked(state.getBoolean("automatic"));
   org.json.JSONArray choices=state.getJSONArray("choices");
   for(int i=0;i<choices.length();i++){org.json.JSONObject c=choices.getJSONObject(i);if(c.getString("id").equals(state.getString("selected")))scenarios.setText("场景 · "+c.getString("name"));}
  } catch(Exception ignored){scenarios.setText("选择场景");automatic.setEnabled(false);}
  finally{refreshingSelection=false;}
 }
 @Override protected void onResume() { super.onResume(); refreshSelection(); handler.post(refresh); }
 @Override protected void onPause() { handler.removeCallbacks(refresh); super.onPause(); }
}

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
 private final Handler handler = new Handler();
 private final Runnable refresh = new Runnable() { public void run() { status.setText(TunnelService.state+"\n"+TunnelService.probeResult); dial.setActive(TunnelService.active);handler.postDelayed(this, 500); } };
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
  automatic = new Switch(this); automatic.setText("连接前按 Wi-Fi 自动选择");
  automatic.setChecked(true); layout.addView(automatic);
  automatic.setOnCheckedChangeListener((button,enabled)->{
   if(refreshingSelection)return;
   try {MobileSelection.get(this).automatic(enabled);
    if(enabled && checkSelfPermission(android.Manifest.permission.ACCESS_FINE_LOCATION)!=android.content.pm.PackageManager.PERMISSION_GRANTED)
     requestPermissions(new String[]{android.Manifest.permission.ACCESS_FINE_LOCATION},2);
   } catch(Exception e){Toast.makeText(this,"请先导入配置，连接期间不能改变选择",Toast.LENGTH_SHORT).show();refreshSelection();}
  });
  Button config = new Button(this); config.setText("导入运行 Profile"); layout.addView(config);
  config.setOnClickListener(v -> edit());
  Button details=new Button(this);details.setText("连接诊断");layout.addView(details);
  details.setOnClickListener(v->new AlertDialog.Builder(this).setTitle("连接诊断").setItems(new String[]{"记录运行计数","验证 DNS 与 HTTPS"},(d,index)->{if(TunnelService.active)startService(new Intent(this,TunnelService.class).setAction(index==0?"diagnose":"probe"));}).show());
  TextView notice = new TextView(this); notice.setText("开发验收版：支持手动场景与连接前 Wi-Fi 匹配；地区判断、连接中切换和登录仍待完成。"); layout.addView(notice);
  setContentView(scroll);
 }
 private int dp(int value){return Math.round(value*getResources().getDisplayMetrics().density);}
 private void connect() { startForegroundService(new Intent(this,TunnelService.class)); }
 @Override protected void onActivityResult(int request,int result,Intent data) { super.onActivityResult(request,result,data); if(request==1 && result==RESULT_OK) connect(); }
 private void chooseScenario() {
  if(TunnelService.active) {new AlertDialog.Builder(this).setMessage("当前连接保持不变。开发验收版暂需断开后选择场景。").setPositiveButton("知道了",null).show();return;}
  try {
   MobileSelection selection=MobileSelection.get(this);
   org.json.JSONObject state=selection.snapshot();org.json.JSONArray choices=state.getJSONArray("choices");
   String[] names=new String[choices.length()];String[] ids=new String[choices.length()];int selected=-1;
   for(int i=0;i<choices.length();i++){org.json.JSONObject choice=choices.getJSONObject(i);names[i]=choice.getString("name");ids[i]=choice.getString("id");if(ids[i].equals(state.getString("selected")))selected=i;}
   new AlertDialog.Builder(this).setTitle("场景").setSingleChoiceItems(names,selected,(d,index)->{
    try {selection.select(ids[index]);refreshSelection();Toast.makeText(this,"场景已选择",Toast.LENGTH_SHORT).show();d.dismiss();}
    catch(Exception e){Toast.makeText(this,"无法保存场景，原配置保留",Toast.LENGTH_LONG).show();}
   }).setNegativeButton("取消",null).show();
  } catch(Exception e){Toast.makeText(this,"请先导入有效配置",Toast.LENGTH_LONG).show();}
 }
 private void edit() {
  if(TunnelService.active){Toast.makeText(this,"请先断开再编辑配置",Toast.LENGTH_SHORT).show();return;}
  EditText input = new EditText(this); input.setGravity(Gravity.TOP); input.setMinLines(10);
  input.setInputType(android.text.InputType.TYPE_CLASS_TEXT | android.text.InputType.TYPE_TEXT_FLAG_MULTI_LINE | android.text.InputType.TYPE_TEXT_FLAG_NO_SUGGESTIONS);
  new AlertDialog.Builder(this).setTitle("运行 Profile JSON").setView(input).setNegativeButton("取消",null)
   .setPositiveButton("保存",(d,w)-> { try {
    String json=input.getText().toString(); Libbox.generateConnectionPlan(json); new ProfileStore(this).save(json); MobileSelection.invalidate(); refreshSelection();
    Toast.makeText(this,"配置已加密保存",Toast.LENGTH_SHORT).show();
   } catch(Exception e) { Toast.makeText(this,"配置无效或无法保存",Toast.LENGTH_LONG).show(); } }).show();
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

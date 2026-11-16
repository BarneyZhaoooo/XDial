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
 private final Handler handler = new Handler();
 private final Runnable refresh = new Runnable() { public void run() { status.setText(TunnelService.state); handler.postDelayed(this, 500); } };
 @Override public void onCreate(Bundle saved) {
  super.onCreate(saved);
  LinearLayout layout = new LinearLayout(this); layout.setOrientation(1); layout.setPadding(32,64,32,32);
  TextView title = new TextView(this); title.setText("XDial · 开发预览"); title.setTextSize(28); layout.addView(title);
  status = new TextView(this); status.setTextSize(20); layout.addView(status);
  Button dial = new Button(this); dial.setText("◉\n连接 / 断开"); dial.setTextSize(32);
  layout.addView(dial, new LinearLayout.LayoutParams(-1, 280));
  dial.setOnClickListener(v -> {
   v.performHapticFeedback(HapticFeedbackConstants.CONTEXT_CLICK);
   if (TunnelService.active) { startService(new Intent(this,TunnelService.class).setAction("stop")); return; }
   Intent consent = VpnService.prepare(this);
   if (consent != null) startActivityForResult(consent, 1); else connect();
  });
  Button config = new Button(this); config.setText("导入运行 Profile"); layout.addView(config);
  config.setOnClickListener(v -> edit());
  TextView notice = new TextView(this); notice.setText("开发验收版：尚未提供自动场景及登录界面。仅使用隔离测试配置。"); layout.addView(notice);
  setContentView(layout);
 }
 private void connect() { startForegroundService(new Intent(this,TunnelService.class)); }
 @Override protected void onActivityResult(int request,int result,Intent data) { super.onActivityResult(request,result,data); if(request==1 && result==RESULT_OK) connect(); }
 private void edit() {
  EditText input = new EditText(this); input.setGravity(Gravity.TOP); input.setMinLines(10);
  input.setInputType(android.text.InputType.TYPE_CLASS_TEXT | android.text.InputType.TYPE_TEXT_FLAG_MULTI_LINE | android.text.InputType.TYPE_TEXT_FLAG_NO_SUGGESTIONS);
  new AlertDialog.Builder(this).setTitle("运行 Profile JSON").setView(input).setNegativeButton("取消",null)
   .setPositiveButton("保存",(d,w)-> { try {
    String json=input.getText().toString(); Libbox.generateConnectionPlan(json); new ProfileStore(this).save(json);
    Toast.makeText(this,"配置已加密保存",Toast.LENGTH_SHORT).show();
   } catch(Exception e) { Toast.makeText(this,"配置无效或无法保存",Toast.LENGTH_LONG).show(); } }).show();
 }
 @Override protected void onResume() { super.onResume(); handler.post(refresh); }
 @Override protected void onPause() { handler.removeCallbacks(refresh); super.onPause(); }
}

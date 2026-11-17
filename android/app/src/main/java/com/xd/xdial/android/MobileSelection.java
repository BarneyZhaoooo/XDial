package com.xd.xdial.android;

import android.content.Context;
import android.os.SystemClock;
import libbox.Libbox;
import libbox.ScenarioController;
import org.json.JSONObject;

/** Process-scoped host adapter. Business decisions belong to the Go controller;
 * storage and the actual tunnel transaction remain Android responsibilities. */
final class MobileSelection {
 private static MobileSelection instance;
 private final Context context;
 private final ScenarioController controller;
 private MobileSelection(Context context) throws Exception {
  this.context=context.getApplicationContext();
  String profile=new ProfileStore(context).load();
  JSONObject regions=new JSONObject(profile).optJSONObject("region_scenarios");
  controller=Libbox.newScenarioController(profile,regions==null?"{}":regions.toString(),
    new JSONObject(profile).optBoolean("mobile_automatic",context.getSharedPreferences("selection",0).getBoolean("automatic",true)));
 }
 static synchronized MobileSelection get(Context context) throws Exception {
  if(instance==null) instance=new MobileSelection(context); return instance;
 }
 static synchronized void invalidate() {instance=null;}
 synchronized JSONObject snapshot() throws Exception {return new JSONObject(controller.snapshot());}
 synchronized void connected(boolean value) throws Exception {controller.setConnected(value);}
 synchronized void automatic(boolean enabled) throws Exception {
  boolean previous=snapshot().getBoolean("automatic");
  controller.setAutomatic(enabled);
  try {
   JSONObject saved=new JSONObject(new ProfileStore(context).load());saved.put("mobile_automatic",enabled);
   new ProfileStore(context).saveSelection(saved.toString());
  } catch(Exception failure) {controller.setAutomatic(previous);throw failure;}
 }
 synchronized void select(String id) throws Exception {
  // Never let two independently started gVisor stacks read one TUN. Until the
  // stable mobile ingress transaction exists, running switches are unavailable.
  if(TunnelService.active) throw new IllegalStateException("connected");
  persistProposal(new JSONObject(controller.selectScenario(id)));
 }
 synchronized void observeBeforeConnect(String ssid) throws Exception {
  long ticket=controller.beginSurvey();
  persistProposal(new JSONObject(controller.completeSurvey(ticket,ssid,"",SystemClock.elapsedRealtime())));
 }
 private void persistProposal(JSONObject result) throws Exception {
  JSONObject pending=result.optJSONObject("pending");if(pending==null)return;
  long token=pending.getLong("token");
  try {
   JSONObject saved=new JSONObject(controller.profileForProposal(token));
   saved.put("mobile_automatic",result.getBoolean("automatic"));
   new ProfileStore(context).saveSelection(saved.toString());
   controller.commit(token,SystemClock.elapsedRealtime());

  } catch(Exception e) {controller.abort(token);throw e;}
 }
}

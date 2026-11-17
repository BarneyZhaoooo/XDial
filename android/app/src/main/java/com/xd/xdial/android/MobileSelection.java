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
  // The service owns connected proposals and their dataplane commit.
  if(TunnelService.active) throw new IllegalStateException("connected");
  JSONObject result=new JSONObject(controller.selectScenario(id));
  if(result.optJSONObject("pending")==null)automatic(result.getBoolean("automatic"));
  else persistProposal(result);
 }
 synchronized long proposeConnected(String id) throws Exception {
  JSONObject result=new JSONObject(controller.selectScenario(id));
  JSONObject pending=result.optJSONObject("pending");
  if(pending==null){automatic(result.getBoolean("automatic"));return 0;}
  // Manual intent disables auto even when candidate preparation later fails.
  try {
   JSONObject old=new JSONObject(new ProfileStore(context).load());
   old.put("mobile_automatic",result.getBoolean("automatic"));
   new ProfileStore(context).saveSelection(old.toString());
  } catch(Exception failure){controller.abort(pending.getLong("token"));throw failure;}
  return pending.getLong("token");
 }
 synchronized String candidate(long token) throws Exception {
  JSONObject result=new JSONObject(controller.profileForProposal(token));
  result.put("mobile_automatic",snapshot().getBoolean("automatic"));return result.toString();
 }
 synchronized void commitConnected(long token,libbox.Libbox_ runtime) throws Exception {
  ProfileStore store=new ProfileStore(context);String before=store.load();
  String target=candidate(token); // Verify the still-current proposal before storage.
  store.saveSelection(target); // Atomic encrypted write must succeed before adoption.
  try {runtime.commitPreparedSwitch();}
  catch(Exception failure){
   try{store.saveSelection(before);}catch(Exception rollback){throw new SelectionRollbackException(rollback);}
   throw failure;
  }
  controller.commit(token,SystemClock.elapsedRealtime());
 }
 synchronized void commitReconnect(long token) throws Exception {
  new ProfileStore(context).saveSelection(candidate(token));controller.commit(token,SystemClock.elapsedRealtime());
 }
 synchronized void abortConnected(long token) throws Exception {controller.abort(token);}
 static final class SelectionRollbackException extends Exception {
  SelectionRollbackException(Exception cause){super(cause);}
 }
 synchronized long beginSurvey() {return controller.beginSurvey();}
 synchronized boolean needsCountry(String ssid) {return controller.needsCountrySurvey(ssid);}
 synchronized void observeBeforeConnect(long ticket,String ssid,String country) throws Exception {
  persistProposal(new JSONObject(controller.completeSurvey(ticket,ssid,country,SystemClock.elapsedRealtime())));
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

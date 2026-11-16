package com.xd.xdial.android;

import android.net.ConnectivityManager;
import android.net.NetworkCapabilities;
import java.net.*;
import java.io.InputStream;
import javax.net.ssl.HttpsURLConnection;

/** Development acceptance probes use ordinary app sockets, deliberately not
 * protected or bound to the Underlay. No configuration or credentials are read. */
final class ConnectionProbe {
 static String run(ConnectivityManager manager) {
  NetworkCapabilities capabilities=manager.getNetworkCapabilities(manager.getActiveNetwork());
  if(capabilities==null || !capabilities.hasTransport(NetworkCapabilities.TRANSPORT_VPN)) return "VPN 未承载测试请求";
  boolean dns=false, https=false;
  // UDP DNS query for a reserved nonexistent name. Accept only matching ID,
  // response bit and NXDOMAIN; a merely writable socket proves nothing.
  byte[] query = new byte[]{0x58,0x44,1,0,0,1,0,0,0,0,0,0,5,'x','d','i','a','l',7,'i','n','v','a','l','i','d',0,0,1,0,1};
  try(DatagramSocket socket=new DatagramSocket()) {
   socket.setSoTimeout(5000);
   socket.connect(InetAddress.getByName("198.18.0.2"),53);
   socket.send(new DatagramPacket(query,query.length));
   byte[] answer=new byte[512]; DatagramPacket response=new DatagramPacket(answer,answer.length); socket.receive(response);
   dns=response.getLength()>=12 && answer[0]==0x58 && answer[1]==0x44 && (answer[2]&0x80)!=0 && (answer[3]&15)==3;
  } catch(Exception ignored) { }
  HttpsURLConnection request=null;
  try {
   request=(HttpsURLConnection)new URL("https://www.apple.com/library/test/success.html").openConnection();
   request.setConnectTimeout(5000); request.setReadTimeout(5000); request.setUseCaches(false);
   try(InputStream body=request.getInputStream()) { https=request.getResponseCode()==200 && body.read()!=-1; }
  } catch(Exception ignored) { }
  finally {if(request!=null) request.disconnect();}
  return "UDP DNS="+(dns?"通过":"失败")+"；HTTPS="+(https?"通过":"失败");
 }
}

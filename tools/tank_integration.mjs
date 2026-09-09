import assert from 'node:assert/strict';
import {randomUUID} from 'node:crypto';

const base=process.env.NAKAMA_URL || 'http://127.0.0.1:7350';
async function request(path,auth,body){
 const response=await fetch(base+path,{method:'POST',headers:{'Content-Type':'application/json',Authorization:auth},body:JSON.stringify(body),signal:AbortSignal.timeout(12000)});
 const data=await response.json(); if(!response.ok)throw Error(data.message || JSON.stringify(data)); return data;
}
async function guest(){
 const auth=await request('/v2/account/authenticate/device?create=true','Basic '+Buffer.from('defaultkey:').toString('base64'),{id:randomUUID()});
 return {token:auth.token,id:JSON.parse(Buffer.from(auth.token.split('.')[1],'base64url')).uid};
}
async function tank(user,body={action:'view'}){return JSON.parse((await request('/v2/rpc/tank','Bearer '+user.token,JSON.stringify(body))).payload);}
function action(name,view,hero=1,partner=2){return {action:name,owner:view.tank.owner,hero_id:hero,partner_id:partner,version:view.tank.version,self_version:view.self.version};}

for(const operation of ['sell','upgrade','breed','breed-partner']){
 const [owner,thief]=await Promise.all([guest(),guest()]);
 assert.notEqual(owner.id,thief.id);
 let own=await tank(owner);
 // Fund breeding/upgrading without test-only server paths.
 own=await tank(owner,action('sell',own,5));
 own=await tank(owner,action('sell',own,4));
 const visit=await tank(thief,{action:'view',owner:owner.id});
 const target=operation==='breed-partner'?2:1;
 const outcomes=await Promise.allSettled([
  tank(owner,action(operation==='breed-partner'?'breed':operation,own)),
  tank(thief,action('steal',visit,target))
 ]);
 assert.equal(outcomes.filter(r=>r.status==='fulfilled').length,1,`${operation}: exactly one concurrent operation must succeed`);
 const [afterOwner,afterThief]=await Promise.all([tank(owner),tank(thief)]);
 const stolen=outcomes[1].status==='fulfilled';
 assert.equal(afterThief.self.heroes.length,stolen?6:5);
 assert.equal(afterOwner.self.heroes.length,stolen||operation==='sell'?2:operation.startsWith('breed')?4:3);
 assert.equal(afterOwner.self.coins,stolen?50:operation==='sell'?75:operation==='upgrade'?30:20);
 console.log(`PASS: concurrent theft / ${operation}; winner=${stolen?'theft':'owner'}`);
}

const [victim,thief]=await Promise.all([guest(),guest()]);
await tank(victim);
const socket=new WebSocket(base.replace(/^http/,'ws')+'/ws?lang=en&status=false&format=json&token='+encodeURIComponent(victim.token));
try {
 await new Promise((resolve,reject)=>{socket.addEventListener('open',resolve,{once:true});socket.addEventListener('error',reject,{once:true});});
 const notification=new Promise((resolve,reject)=>{
  const timeout=setTimeout(()=>reject(Error('Victim did not receive realtime theft notification')),3000);
  socket.addEventListener('message',event=>{
   const packet=JSON.parse(event.data);
   if(packet.notifications?.notifications?.some(n=>n.code===101)){clearTimeout(timeout);resolve();}
  });
 });
 const visit=await tank(thief,{action:'view',owner:victim.id});
 await tank(thief,action('steal',visit));
 await notification;
 assert.equal((await tank(victim)).self.heroes.some(h=>h.id===1),false);
 console.log('PASS: realtime victim notification and authoritative hero removal');
} finally {socket.close();}

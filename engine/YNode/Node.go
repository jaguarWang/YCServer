package YNode

import (
	"fmt"
	ylog "github.com/yxinyi/YCServer/engine/YLog"
	"github.com/yxinyi/YCServer/engine/YModule"
	"github.com/yxinyi/YCServer/engine/YMsg"
	"reflect"
	"runtime/debug"
	"strings"
	"time"
)

var obj = newInfo()
var g_stop = make(chan struct{})

func init() {
	obj.Info = YModule.NewInfo(obj)
}

func (n *Info) findNode(node_id_ uint32) uint64 {
	return n.M_node_id_to_session[node_id_]
}

func (n *Info) GetModuleName(module_name_ string, module_uid_ uint64) string {
	return fmt.Sprintf("%s:%d", module_name_, module_uid_)
}

func (n *Info) register(info YModule.Inter) {
	info.GetInfo().M_name = strings.Split(reflect.TypeOf(info).Elem().String(), ".")[0]
	
	obj.M_module_pool[n.GetModuleName(info.GetInfo().M_name, info.GetInfo().M_module_uid)] = info
	info.Init()
}

func (n *Info) RPCToOther(msg *YMsg.S2S_rpc_msg) {
	obj.PushRpcMsg(msg)
}
func (n *Info) NetToOther(msg *YMsg.C2S_net_msg) {
	obj.PushNetMsg(msg)
}

func (n *Info) dispatchNet(msg_ *YMsg.C2S_net_msg) bool {
	_module_name_uid_str := n.GetModuleName(msg_.M_tar.M_name, msg_.M_tar.M_uid)
	
	_, exists := obj.M_module_pool[_module_name_uid_str]
	if !exists {
		ylog.Erro("[YNode:dispatchRpc] miss module uid [%v]", _module_name_uid_str)
		return false
	}
	obj.M_module_pool[_module_name_uid_str].GetInfo().PushNetMsg(msg_)
	return true
}

func (n *Info) dispatchRpc(msg_ *YMsg.S2S_rpc_msg) bool {
	_module_name_uid_str := n.GetModuleName(msg_.M_tar.M_name, msg_.M_tar.M_uid)
	{
		_, exists := obj.M_module_pool[_module_name_uid_str]
		if !exists {
			ylog.Erro("[YNode:dispatchRpc] miss module uid [%v]", _module_name_uid_str)
			return false
		}
	}
	
	//ylog.Info("[Node:%v] dispatch RPC [%v]", obj.M_module_pool[msg_.M_tar.M_entity_name][msg_.M_tar.M_node_id].GetInfo().M_entity_name, msg_.M_func_name)
	obj.M_module_pool[_module_name_uid_str].GetInfo().PushRpcMsg(msg_)
	return true
}

func (n *Info) close() {
	for _, _module_it := range obj.M_module_pool {
		_module_it.Close()
	}
}
func (n *Info) startModule(module_ YModule.Inter) {
	defer func() {
		if err := recover(); err != nil {
			ylog.Erro("%v", err)
			ylog.Erro("stack:%s", debug.Stack())
		}
	}()
	
	_100_last_print_time := time.Now().Unix()
	_10_last_print_time := time.Now().Unix()
	_1_last_print_time := time.Now().Unix()
	_100_fps_count := 0
	_10_fps_count := 0
	_1_fps_count := 0
	
	///////////
	
	_100_fps_timer := time.NewTicker(time.Millisecond * 10)
	defer _100_fps_timer.Stop()
	_10_fps_timer := time.NewTicker(time.Millisecond * 100)
	defer _10_fps_timer.Stop()
	_1_fps_timer := time.NewTicker(time.Millisecond * 1000)
	defer _10_fps_timer.Stop()
	for {
		select {
		case _time := <-_100_fps_timer.C:
			_100_fps_count++
			module_.Loop_100(_time)
			module_.GetInfo().Loop_Msg()
			if (_time.Unix() - _100_last_print_time) >= 60 {
				_second_fps := _100_fps_count / int(_time.Unix()-_100_last_print_time)
				if _second_fps < 80 {
					ylog.Erro("[Module:%v] 100 fps [%v]", module_.GetInfo().M_name, _100_fps_count/int(_time.Unix()-_100_last_print_time))
				}
				_100_last_print_time = _time.Unix()
				_100_fps_count = 0
			}
		case _time := <-_10_fps_timer.C:
			_10_fps_count++
			module_.Loop_10(_time)
			if (_time.Unix() - _10_last_print_time) >= 60 {
				_second_fps := _10_fps_count / int(_time.Unix()-_10_last_print_time)
				if _second_fps < 8 {
					ylog.Info("[Module:%v] 10 fps [%v]", module_.GetInfo().M_name, _10_fps_count/int(_time.Unix()-_10_last_print_time))
				}
				
				_10_last_print_time = _time.Unix()
				_10_fps_count = 0
			}
		case _time := <-_1_fps_timer.C:
			_1_fps_count++
			module_.Loop_1(_time)
			if (_time.Unix() - _1_last_print_time) >= 60 {
				_second_fps := _1_fps_count / int(_time.Unix()-_1_last_print_time)
				if _second_fps < 1 {
					ylog.Info("[Module:%v] 10 fps [%v]", module_.GetInfo().M_name, _1_fps_count/int(_time.Unix()-_1_last_print_time))
				}
				
				_1_last_print_time = _time.Unix()
				_1_fps_count = 0
			}
		}
		
	}
}
func (n *Info) start() {
	for _, _module_it := range obj.M_module_pool {
		go n.startModule(_module_it)
	}
	//主逻辑
	obj.register(obj)
	obj.GetInfo().Init(obj)
	n.loop()
}

func (n *Info) loop() {
	for {
		select {
		case <-g_stop:
			return
		default:
			
			if obj.M_net_queue.Len() > 0 {
				for {
					if obj.M_net_queue.Len() == 0 {
						break
					}
					_msg := obj.M_net_queue.Pop().(*YMsg.C2S_net_msg)
					
					if n.dispatchNet(_msg) {
						continue
					}
					
					obj.Info.SendNetMsgJson(n.findNode(_msg.M_tar.M_node_id), *_msg)
				}
			}
			if obj.M_rpc_queue.Len() > 0 {
				for {
					if obj.M_rpc_queue.Len() == 0 {
						break
					}
					//ylog.Info("[Node:RPC_QUEUE] [%v]", obj.M_rpc_queue.Len())
					_msg := obj.M_rpc_queue.Pop().(*YMsg.S2S_rpc_msg)
					if _msg.M_tar.M_name == "YNode" {
						n.DoRPCMsg(_msg)
						continue
					}
					if n.dispatchRpc(_msg) {
						continue
					}
					obj.Info.SendNetMsgJson(n.findNode(_msg.M_tar.M_node_id), *_msg)
				}
			}
		}
	}
}

func Register(info_list_ ...YModule.Inter) {
	for _, _it := range info_list_ {
		obj.register(_it)
	}
}

func RPCCall(msg_ *YMsg.S2S_rpc_msg) {
	
	obj.RPCToOther(msg_)
}

func Obj() *Info {
	return obj
}

func Close() {
	obj.close()
}

func Start() {
	obj.start()
}

func ModuleCreateFuncRegister(name_ string, func_ func(node_ *Info, uid uint64) YModule.Inter) {
	obj.registerToFactory(name_, func_)
}

func (n *Info) registerToFactory(name_ string, func_ func(node_ *Info, uid uint64) YModule.Inter) {
	n.m_moduele_factory[name_] = func_
}

func RegisterNodeIpStr2NodeId(ip_port_ string, node_id_ uint32) {
	obj.M_node_ip_to_id[ip_port_] = node_id_
}

func SetNodeID(node_id_ uint32) {
	obj.GetInfo().M_node_id = node_id_
}

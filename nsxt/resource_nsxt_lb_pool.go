/* Copyright © 2018 VMware, Inc. All Rights Reserved.
   SPDX-License-Identifier: MPL-2.0 */

package nsxt

import (
	"fmt"
	"github.com/hashicorp/terraform/helper/schema"
	"github.com/hashicorp/terraform/helper/validation"
	api "github.com/vmware/go-vmware-nsxt"
	"github.com/vmware/go-vmware-nsxt/common"
	"github.com/vmware/go-vmware-nsxt/loadbalancer"
	"log"
	"net/http"
)

var poolAlgTypeValues = []string{"ROUND_ROBIN", "WEIGHTED_ROUND_ROBIN", "LEAST_CONNECTION", "WEIGHTED_LEAST_CONNECTION", "IP_HASH"}
var poolSnatTranslationTypeValues = []string{"SNAT_AUTO_MAP", "SNAT_IP_POOL", "TRANSPARENT"}
var memberAdminStateTypeValues = []string{"ENABLED", "DISABLED", "GRACEFUL_DISABLED"}
var ipRevisionFilterTypeValues = []string{"IPV4", "IPV6"}

func resourceNsxtLbPool() *schema.Resource {
	return &schema.Resource{
		Create: resourceNsxtLbPoolCreate,
		Read:   resourceNsxtLbPoolRead,
		Update: resourceNsxtLbPoolUpdate,
		Delete: resourceNsxtLbPoolDelete,
		Importer: &schema.ResourceImporter{
			State: schema.ImportStatePassthrough,
		},

		Schema: map[string]*schema.Schema{
			"revision": getRevisionSchema(),
			"description": &schema.Schema{
				Type:        schema.TypeString,
				Description: "Description of this resource",
				Optional:    true,
			},
			"display_name": &schema.Schema{
				Type:        schema.TypeString,
				Description: "The display name of this resource. Defaults to ID if not set",
				Optional:    true,
				Computed:    true,
			},
			"tag": getTagsSchema(),
			"algorithm": &schema.Schema{
				Type:         schema.TypeString,
				Description:  "Load balancing algorithm controls how the incoming connections are distributed among the members",
				ValidateFunc: validation.StringInSlice(poolAlgTypeValues, false),
				Optional:     true,
				Default:      "ROUND_ROBIN",
			},
			"min_active_members": &schema.Schema{
				Type:        schema.TypeInt,
				Description: "The minimum number of members for the pool to be considered active",
				Optional:    true,
				Default:     1,
			},
			"tcp_multiplexing_enabled": &schema.Schema{
				Type:        schema.TypeBool,
				Description: "TCP multiplexing allows the same TCP connection between load balancer and the backend server to be used for sending multiple client requests from different client TCP connections",
				Optional:    true,
				Default:     false,
			},
			"tcp_multiplexing_number": &schema.Schema{
				Type:        schema.TypeInt,
				Description: "The maximum number of TCP connections per pool that are idly kept alive for sending future client requests",
				Optional:    true,
				Default:     6,
			},
			"active_monitor_id": &schema.Schema{
				Type:        schema.TypeString,
				Description: "Active health monitor Id. If one is not set, the active healthchecks will be disabled",
				Optional:    true,
			},
			"passive_monitor_id": &schema.Schema{
				Type:        schema.TypeString,
				Description: "Passive health monitor Id. If one is not set, the passive healthchecks will be disabled",
				Optional:    true,
			},
			"snat_translation": getSnatTranslationSchema(),
			"member":           getPoolMembersSchema(),
			"member_group":     getPoolMemberGroupSchema(),
		},
	}
}

func getSnatTranslationSchema() *schema.Schema {
	return &schema.Schema{
		Type:        schema.TypeList,
		Description: "SNAT translation configuration",
		Optional:    true,
		Computed:    true,
		MaxItems:    1,
		Elem: &schema.Resource{
			Schema: map[string]*schema.Schema{
				"type": &schema.Schema{
					Type:         schema.TypeString,
					Description:  "Type of SNAT performed to ensure reverse traffic from the server can be received and processed by the loadbalancer",
					ValidateFunc: validation.StringInSlice(poolSnatTranslationTypeValues, false),
					Optional:     true,
					Default:      "TRANSPARENT",
				},
				"ip": &schema.Schema{
					Type:         schema.TypeString,
					Description:  "Ip address or Ip range for SNAT of type SNAT_IP_POOL",
					ValidateFunc: validateIPOrRange(),
					Optional:     true,
				},
				"port_overload": &schema.Schema{
					Type:         schema.TypeInt,
					Description:  "Maximum times for reusing the same SNAT IP and port for multiple backend connections",
					ValidateFunc: validatePowerOf2(true, 32),
					Optional:     true,
					Computed:     true,
				},
			},
		},
	}
}

func getPoolMembersSchema() *schema.Schema {
	return &schema.Schema{
		Type:        schema.TypeList,
		Description: "List of server pool members. Each pool member is identified, typically, by an IP address and a port",
		Optional:    true,
		Elem: &schema.Resource{
			Schema: map[string]*schema.Schema{
				"display_name": &schema.Schema{
					Type:        schema.TypeString,
					Description: "Pool member name",
					Optional:    true,
					Computed:    true,
				},
				"admin_state": &schema.Schema{
					Type:         schema.TypeString,
					Description:  "Member admin state",
					Optional:     true,
					ValidateFunc: validation.StringInSlice(memberAdminStateTypeValues, false),
					Default:      "ENABLED",
				},
				"backup_member": &schema.Schema{
					Type:        schema.TypeBool,
					Description: "A boolean flag which reflects whether this is a backup pool member",
					Optional:    true,
					Default:     false,
				},
				"ip_address": &schema.Schema{
					Type:         schema.TypeString,
					Description:  "Pool member IP address",
					Required:     true,
					ValidateFunc: validateSingleIP(),
				},
				"max_concurrent_connections": &schema.Schema{
					Type:        schema.TypeInt,
					Description: "To ensure members are not overloaded, connections to a member can be capped by the load balancer. When a member reaches this limit, it is skipped during server selection. If it is not specified, it means that connections are unlimited",
					Optional:    true,
				},
				"port": &schema.Schema{
					Type:         schema.TypeString,
					Description:  "If port is specified, all connections will be sent to this port. Only single port is supported. If unset, the same port the client connected to will be used, it could be overrode by default_pool_member_port setting in virtual server. The port should not specified for port range case",
					Optional:     true,
					ValidateFunc: validateSinglePort(),
				},
				"weight": &schema.Schema{
					Type:        schema.TypeInt,
					Description: "Pool member weight is used for WEIGHTED_ROUND_ROBIN balancing algorithm. The weight value would be ignored in other algorithms",
					Optional:    true,
					Default:     1,
				},
			},
		},
	}
}

func getPoolMemberGroupSchema() *schema.Schema {
	return &schema.Schema{
		Type:        schema.TypeList,
		Description: "Dynamic pool members for the loadbalancing pool. When member group is defined, members setting should not be specified",
		Optional:    true,
		MaxItems:    1,
		Elem: &schema.Resource{
			Schema: map[string]*schema.Schema{
				"grouping_object": getSingleResourceReferencesSchema(true, false, []string{"NSGroup"}, "Load balancer pool support grouping object as dynamic pool members. The IP list of the grouping object such as NSGroup would be used as pool member IP setting"),
				"ip_version_filter": &schema.Schema{
					Type:         schema.TypeString,
					Description:  "Ip revision filter is used to filter IPv4 or IPv6 addresses from the grouping object. If the filter is not specified, both IPv4 and IPv6 addresses would be used as server IPs",
					Optional:     true,
					ValidateFunc: validation.StringInSlice(ipRevisionFilterTypeValues, false),
					Default:      "IPV4",
				},
				"limit_ip_list_size": &schema.Schema{
					Type:        schema.TypeBool,
					Description: "Specifies whether to limit pool members. If false, dynamic pool can grow up to the load balancer max pool member capacity.",
					Optional:    true,
					Default:     false,
				},
				"max_ip_list_size": &schema.Schema{
					Type:        schema.TypeInt,
					Description: "Limits the max number of pool members to the specified value if limit_ip_list_size is set to true, ignored otherwise.",
					Optional:    true,
				},
				"port": &schema.Schema{
					Type:        schema.TypeInt,
					Description: "If port is specified, all connections will be sent to this port. If unset, the same port the client connected to will be used",
					Optional:    true,
				},
			},
		},
	}
}

func getActiveMonitorIdsFromSchema(d *schema.ResourceData) []string {
	activeMonitorID := d.Get("active_monitor_id").(string)
	var activeMonitorIds []string
	if activeMonitorID != "" {
		activeMonitorIds = append(activeMonitorIds, activeMonitorID)
	}
	return activeMonitorIds
}

func getSnatTranslationFromSchema(d *schema.ResourceData) *loadbalancer.LbSnatTranslation {
	snatConfs := d.Get("snat_translation").([]interface{})
	for _, snatConf := range snatConfs {
		// only 1 snat_translation is allowed so return the first 1
		data := snatConf.(map[string]interface{})

		// TRANSPARENT type should not create an object
		trType := data["type"].(string)
		if trType == "TRANSPARENT" {
			return nil
		}
		portOverload := int64(data["port_overload"].(int))
		if trType == "SNAT_IP_POOL" {
			ipAddresses := make([]loadbalancer.LbSnatIpElement, 0, 1)
			ipAddress := data["ip"].(string)
			elem := loadbalancer.LbSnatIpElement{IpAddress: ipAddress}
			ipAddresses = append(ipAddresses, elem)
			return &loadbalancer.LbSnatTranslation{
				Type_:        "LbSnatIpPool",
				IpAddresses:  ipAddresses,
				PortOverload: portOverload,
			}
		}
		// For SNAT_AUTO_MAP type
		return &loadbalancer.LbSnatTranslation{
			Type_:        "LbSnatAutoMap",
			PortOverload: portOverload,
		}
	}
	return nil
}

func setSnatTranslationInSchema(d *schema.ResourceData, snatTranslation *loadbalancer.LbSnatTranslation) error {
	var snatTranslationList []map[string]interface{}
	elem := make(map[string]interface{})
	if snatTranslation != nil {
		if snatTranslation.Type_ == "LbSnatIpPool" {
			elem["type"] = "SNAT_IP_POOL"
			if snatTranslation.IpAddresses != nil && len(snatTranslation.IpAddresses) > 0 {
				elem["ip"] = snatTranslation.IpAddresses[0].IpAddress
			}
		} else {
			elem["type"] = "SNAT_AUTO_MAP"
		}
		elem["port_overload"] = snatTranslation.PortOverload
	} else {
		elem["type"] = "TRANSPARENT"
	}
	snatTranslationList = append(snatTranslationList, elem)
	err := d.Set("snat_translation", snatTranslationList)
	return err
}

func setPoolMembersInSchema(d *schema.ResourceData, members []loadbalancer.PoolMember) error {
	var membersList []map[string]interface{}
	for _, member := range members {
		elem := make(map[string]interface{})
		elem["display_name"] = member.DisplayName
		elem["admin_state"] = member.AdminState
		elem["backup_member"] = member.BackupMember
		elem["ip_address"] = member.IpAddress
		elem["max_concurrent_connections"] = member.MaxConcurrentConnections
		elem["port"] = member.Port
		elem["weight"] = member.Weight

		membersList = append(membersList, elem)
	}
	err := d.Set("member", membersList)
	return err
}

func getPoolMembersFromSchema(d *schema.ResourceData) []loadbalancer.PoolMember {
	members := d.Get("member").([]interface{})
	var memberList []loadbalancer.PoolMember
	for _, member := range members {
		data := member.(map[string]interface{})
		elem := loadbalancer.PoolMember{
			AdminState:   data["admin_state"].(string),
			BackupMember: data["backup_member"].(bool),
			DisplayName:  data["display_name"].(string),
			IpAddress:    data["ip_address"].(string),
			Port:         data["port"].(string),
			Weight:       int64(data["weight"].(int)),
			MaxConcurrentConnections: int64(data["max_concurrent_connections"].(int)),
		}

		memberList = append(memberList, elem)
	}
	return memberList
}

func setPoolGroupMemberInSchema(d *schema.ResourceData, groupMember *loadbalancer.PoolMemberGroup) error {
	var groupMembersList []map[string]interface{}
	if groupMember != nil {
		elem := make(map[string]interface{})
		var refList []common.ResourceReference
		if groupMember.GroupingObject != nil {
			refList = append(refList, *groupMember.GroupingObject)
		}
		elem["grouping_object"] = returnResourceReferences(refList)
		elem["ip_version_filter"] = groupMember.IpRevisionFilter
		if groupMember.MaxIpListSize != nil {
			elem["max_ip_list_size"] = *groupMember.MaxIpListSize
			elem["limit_ip_list_size"] = true
		} else {
			elem["limit_ip_list_size"] = false
		}

		elem["port"] = groupMember.Port

		groupMembersList = append(groupMembersList, elem)
	}
	err := d.Set("member_group", groupMembersList)
	return err
}

func getPoolMemberGroupFromSchema(d *schema.ResourceData) *loadbalancer.PoolMemberGroup {
	memberGroups := d.Get("member_group").([]interface{})
	for _, member := range memberGroups {
		// only 1 member group is allowed so return the first 1
		data := member.(map[string]interface{})
		groupingObject := getSingleResourceReference(data["grouping_object"].([]interface{}))
		memberGroup := loadbalancer.PoolMemberGroup{
			IpRevisionFilter: data["ip_version_filter"].(string),
			Port:             int32(data["port"].(int)),
			GroupingObject:   groupingObject,
		}
		memberGroup.MaxIpListSize = nil
		if data["limit_ip_list_size"].(bool) {
			maxSize := int64(data["max_ip_list_size"].(int))
			memberGroup.MaxIpListSize = &maxSize
		}
		return &memberGroup
	}
	return nil
}

func resourceNsxtLbPoolCreate(d *schema.ResourceData, m interface{}) error {
	nsxClient := m.(*api.APIClient)
	description := d.Get("description").(string)
	displayName := d.Get("display_name").(string)
	tags := getTagsFromSchema(d)
	activeMonitorIds := getActiveMonitorIdsFromSchema(d)
	passiveMonitorID := d.Get("passive_monitor_id").(string)
	algorithm := d.Get("algorithm").(string)
	members := getPoolMembersFromSchema(d)
	memberGroup := getPoolMemberGroupFromSchema(d)
	minActiveMembers := int64(d.Get("min_active_members").(int))
	tcpMultiplexingEnabled := d.Get("tcp_multiplexing_enabled").(bool)
	tcpMultiplexingNumber := int64(d.Get("tcp_multiplexing_number").(int))
	snatTranslation := getSnatTranslationFromSchema(d)
	lbPool := loadbalancer.LbPool{
		Description:            description,
		DisplayName:            displayName,
		Tags:                   tags,
		ActiveMonitorIds:       activeMonitorIds,
		Algorithm:              algorithm,
		MinActiveMembers:       minActiveMembers,
		PassiveMonitorId:       passiveMonitorID,
		SnatTranslation:        snatTranslation,
		TcpMultiplexingEnabled: tcpMultiplexingEnabled,
		TcpMultiplexingNumber:  tcpMultiplexingNumber,
		Members:                members,
		MemberGroup:            memberGroup,
	}

	lbPool, resp, err := nsxClient.ServicesApi.CreateLoadBalancerPool(nsxClient.Context, lbPool)

	if err != nil {
		return fmt.Errorf("Error during LbPool create: %v", err)
	}

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Unexpected status returned during LbPool create: %v", resp.StatusCode)
	}
	d.SetId(lbPool.Id)

	return resourceNsxtLbPoolRead(d, m)
}

func resourceNsxtLbPoolRead(d *schema.ResourceData, m interface{}) error {
	nsxClient := m.(*api.APIClient)
	id := d.Id()
	if id == "" {
		return fmt.Errorf("Error obtaining logical object id")
	}

	lbPool, resp, err := nsxClient.ServicesApi.ReadLoadBalancerPool(nsxClient.Context, id)
	if err != nil {
		return fmt.Errorf("Error during LbPool read: %v", err)
	}
	if resp.StatusCode == http.StatusNotFound {
		log.Printf("[DEBUG] LbPool %s not found", id)
		d.SetId("")
		return nil
	}

	d.Set("revision", lbPool.Revision)
	d.Set("description", lbPool.Description)
	d.Set("display_name", lbPool.DisplayName)
	setTagsInSchema(d, lbPool.Tags)
	if lbPool.ActiveMonitorIds != nil && len(lbPool.ActiveMonitorIds) > 0 {
		d.Set("active_monitor_id", lbPool.ActiveMonitorIds[0])
	} else {
		d.Set("active_monitor_id", "")
	}
	d.Set("passive_monitor_id", lbPool.PassiveMonitorId)
	d.Set("algorithm", lbPool.Algorithm)
	err = setPoolMembersInSchema(d, lbPool.Members)
	if err != nil {
		return fmt.Errorf("Error during LB Pool members set in schema: %v", err)
	}
	err = setPoolGroupMemberInSchema(d, lbPool.MemberGroup)
	if err != nil {
		return fmt.Errorf("Error during LB Pool group member set in schema: %v", err)
	}
	d.Set("min_active_members", lbPool.MinActiveMembers)
	err = setSnatTranslationInSchema(d, lbPool.SnatTranslation)
	if err != nil {
		return fmt.Errorf("Error during LB Pool SNAT translation set in schema: %v", err)
	}
	d.Set("tcp_multiplexing_enabled", lbPool.TcpMultiplexingEnabled)
	d.Set("tcp_multiplexing_number", lbPool.TcpMultiplexingNumber)

	return nil
}

func resourceNsxtLbPoolUpdate(d *schema.ResourceData, m interface{}) error {
	nsxClient := m.(*api.APIClient)
	id := d.Id()
	if id == "" {
		return fmt.Errorf("Error obtaining logical object id")
	}

	revision := int32(d.Get("revision").(int))
	description := d.Get("description").(string)
	displayName := d.Get("display_name").(string)
	tags := getTagsFromSchema(d)
	activeMonitorIds := getActiveMonitorIdsFromSchema(d)
	passiveMonitorID := d.Get("passive_monitor_id").(string)
	algorithm := d.Get("algorithm").(string)
	members := getPoolMembersFromSchema(d)
	memberGroup := getPoolMemberGroupFromSchema(d)
	minActiveMembers := int64(d.Get("min_active_members").(int))
	snatTranslation := getSnatTranslationFromSchema(d)
	tcpMultiplexingEnabled := d.Get("tcp_multiplexing_enabled").(bool)
	tcpMultiplexingNumber := int64(d.Get("tcp_multiplexing_number").(int))
	lbPool := loadbalancer.LbPool{
		Revision:               revision,
		Description:            description,
		DisplayName:            displayName,
		Tags:                   tags,
		ActiveMonitorIds:       activeMonitorIds,
		Algorithm:              algorithm,
		MinActiveMembers:       minActiveMembers,
		PassiveMonitorId:       passiveMonitorID,
		SnatTranslation:        snatTranslation,
		TcpMultiplexingEnabled: tcpMultiplexingEnabled,
		TcpMultiplexingNumber:  tcpMultiplexingNumber,
		Members:                members,
		MemberGroup:            memberGroup,
	}

	lbPool, resp, err := nsxClient.ServicesApi.UpdateLoadBalancerPool(nsxClient.Context, id, lbPool)

	if err != nil || resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("Error during LbPool update: %v", err)
	}

	return resourceNsxtLbPoolRead(d, m)
}

func resourceNsxtLbPoolDelete(d *schema.ResourceData, m interface{}) error {
	nsxClient := m.(*api.APIClient)
	id := d.Id()
	if id == "" {
		return fmt.Errorf("Error obtaining logical object id")
	}

	resp, err := nsxClient.ServicesApi.DeleteLoadBalancerPool(nsxClient.Context, id)
	if err != nil {
		return fmt.Errorf("Error during LbPool delete: %v", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		log.Printf("[DEBUG] LbPool %s not found", id)
		d.SetId("")
	}
	return nil
}

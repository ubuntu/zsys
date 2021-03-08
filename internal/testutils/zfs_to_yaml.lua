-- lua "program" (to be used with "zfs program") to convert a pool into the yaml test format used by zsys
-- syntax, e.g.:
-- sudo zfs program -n rpool zfs_to_yaml.lua -- rpool/ROOT rpool/USERDATA

-- list of filesystems' properties to dump to yaml
fs_prop = {{prop='mountpoint', yaml='mountpoint'},
           {prop='canmount', yaml='canmount'},
           {prop='origin', yaml='origin'},
           {prop='com.ubuntu.zsys:bootfs', yaml='zsys_bootfs'},
           {prop='com.ubuntu.zsys:last-used', yaml='last_used', to_date=true},
           {prop='com.ubuntu.zsys:last-booted-kernel', yaml='last_booted_kernel'},
           {prop='com.ubuntu.zsys:bootfs-datasets', yaml='bootfs_datasets'},
    }

-- list of snapshots' properties to dump to yaml
snap_prop = {{prop='creation', yaml='creation_time', to_date=true},
             {prop='com.ubuntu.zsys:mountpoint', yaml='mountpoint'},
             {prop='com.ubuntu.zsys:canmount', yaml='canmount'},
             {prop='com.ubuntu.zsys:bootfs', yaml='zsys_bootfs'},
             {prop='com.ubuntu.zsys:last-booted-kernel', yaml='last_booted_kernel'},
             {prop='com.ubuntu.zsys:bootfs-datasets', yaml='bootfs_datasets'},
    }

-- function to perform pseudo-conversion of unix epoch to a valid date (injective and order-preserving)
-- to be replaced with os.date if/when added to ZFS program
function pseudo_os_date(time)
    return string.format('%04d-%02d-%02dT%02d:%02d:%02dZ', 1970+time/29030400, 1+time%29030400/2419200, 1+time%2419200/86400, time%86400/3600, time%3600/60, time%60)
end

-- function to dump a given property if it exists and it's not default/inherited
function dump_prop(dataset, prepend, prop_name, yaml_name, to_date)
    local value, source = zfs.get_prop(dataset, prop_name)
    if value ~= nil and (source == dataset or source == '$recvd' or source == nil) then
        if to_date == true then value = pseudo_os_date(value) end
        return string.format('%s%s: %q\n', prepend, yaml_name, value)
    end
    return ''
end

-- function recursing through the datasets and dumping the properties of each filesystem and its snapshots
function list_recursive(dataset)
    local ret = ''
    -- dataset name (stripping pool name)
    local name, nrep = string.gsub(dataset, '^[^/]*/', '', 1)
    if nrep == 0 then name = '.' end
    ret = ret .. string.format('      - name: %s\n', name)
    -- dump isvolume if true
    if zfs.get_prop(dataset, 'type') == 'volume' then
        ret = ret .. '        isvolume: true\n'
    end
    -- other properties
    for _, prop in ipairs(fs_prop) do
        ret = ret .. dump_prop(dataset, '        ', prop.prop, prop.yaml, prop.to_date)
    end
    -- iterate over snapshots in creation order
    local ordered_snapshots = {}
    for snap in zfs.list.snapshots(dataset) do
        table.insert(ordered_snapshots, {creation=zfs.get_prop(snap, 'creation'), name=snap})
    end
    table.sort(ordered_snapshots, function(a, b) return a.creation < b.creation end)
    for i, snap in ipairs(ordered_snapshots) do
        if i == 1 then
            ret = ret .. '        snapshots:\n'
        end
        name = string.gsub(snap.name, '^[^@]*@', '', 1)
        ret = ret .. string.format('          - name: %s\n', name)
        for _, prop in ipairs(snap_prop) do
            ret = ret .. dump_prop(snap.name, '            ', prop.prop, prop.yaml, prop.to_date)
        end
    end
    -- recurse over children
    for child in zfs.list.children(dataset) do
        ret = ret .. list_recursive(child)
    end
    return ret
end

-- main
args = ...
argv = args["argv"]
ret = ''
for _, dataset in ipairs(argv) do
    ret = ret .. list_recursive(dataset)
end
return ret
